package auditor

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/storage/basedb"
)

type Store interface {
	PutFinding(f *Finding) (PutResult, error)
	Query(q Query) (QueryResult, error)
	Prune(pruneBefore phase0.Slot) error
}

type PutResult struct {
	Stored bool
	Seq    uint16
	Key    string
}

type Query struct {
	From phase0.Slot
	To   phase0.Slot

	Reason *ReasonCode
	Role   *Role

	CommitteeIDHex *string
	ValidatorIndex *uint64

	Limit int
}

type QueryResult struct {
	Findings []*Finding
}

type DBStore struct {
	db basedb.Database
	// maxPerSlotReason enforces "max 10 per reason per slot" invariant.
	maxPerSlotReason uint16
}

const (
	findingPrefixKey = "af"  // per-slot prefix: af + slotBytes
	countPrefixKey   = "afc" // per-slot prefix: afc + slotBytes; key: reasonByte -> uint16 count
	metaPrefixKey    = "afm"
)

const (
	metaLastPrunedKey = "last_pruned_slot"
	metaMinSlotKey    = "min_slot"
	metaMaxSlotKey    = "max_slot"
)

func NewDBStore(db basedb.Database) *DBStore {
	return &DBStore{db: db, maxPerSlotReason: 10}
}

func findingKey(slot phase0.Slot, reason ReasonCode, seq uint16) string {
	return fmt.Sprintf("%d/%s/%d", uint64(slot), reason, seq)
}

func (s *DBStore) PutFinding(f *Finding) (PutResult, error) {
	if f == nil {
		return PutResult{}, fmt.Errorf("nil finding")
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	if f.Version == 0 {
		f.Version = 1
	}

	slot := phase0.Slot(f.Slot)
	prefixFinding := makeSlotPrefix(findingPrefixKey, slot)
	prefixCount := makeSlotPrefix(countPrefixKey, slot)

	reasonByte := reasonToByte(f.Reason)
	if reasonByte == 0 {
		return PutResult{}, fmt.Errorf("unsupported reason code: %s", f.Reason)
	}

	// Enforce max-per-(slot,reason) across restarts using a persisted counter.
	countKey := []byte{reasonByte}
	obj, found, err := s.db.Get(prefixCount, countKey)
	if err != nil {
		return PutResult{}, fmt.Errorf("get finding count: %w", err)
	}
	var count uint16
	if found {
		if len(obj.Value) != 2 {
			return PutResult{}, fmt.Errorf("invalid finding count encoding")
		}
		count = binary.LittleEndian.Uint16(obj.Value)
	}
	if count >= s.maxPerSlotReason {
		return PutResult{Stored: false}, nil
	}

	// Persist the finding under (slot, reason, seq).
	if count > 255 {
		return PutResult{}, fmt.Errorf("finding count overflow: %d", count)
	}
	// #nosec G115 -- count is bounded by maxPerSlotReason (default 10) and the overflow check above.
	seq := uint8(count) // 0..255
	key := []byte{reasonByte, seq}
	res := PutResult{Stored: true, Seq: count}
	res.Key = findingKey(slot, f.Reason, res.Seq)
	f.Key = res.Key

	value, err := json.Marshal(f)
	if err != nil {
		return PutResult{}, fmt.Errorf("marshal finding: %w", err)
	}
	if err := s.db.Set(prefixFinding, key, value); err != nil {
		return PutResult{}, fmt.Errorf("save finding: %w", err)
	}

	// Increment the count.
	var enc [2]byte
	binary.LittleEndian.PutUint16(enc[:], count+1)
	if err := s.db.Set(prefixCount, countKey, enc[:]); err != nil {
		return PutResult{}, fmt.Errorf("save finding count: %w", err)
	}

	// Best-effort slot bounds for prune initialization.
	_ = s.updateSlotBounds(slot)
	return res, nil
}

func (s *DBStore) Query(q Query) (QueryResult, error) {
	if q.Limit <= 0 {
		q.Limit = 500
	}
	if q.To < q.From {
		return QueryResult{}, fmt.Errorf("'to' must be >= 'from'")
	}

	out := make([]*Finding, 0)
	for slot := q.From; slot <= q.To; slot++ {
		prefixFinding := makeSlotPrefix(findingPrefixKey, slot)
		err := s.db.GetAll(prefixFinding, func(_ int, obj basedb.Obj) error {
			if len(out) >= q.Limit {
				return nil
			}
			f := new(Finding)
			if err := json.Unmarshal(obj.Value, f); err != nil {
				return nil
			}
			if q.Reason != nil && f.Reason != *q.Reason {
				return nil
			}
			if q.Role != nil {
				if f.Role == nil || *f.Role != *q.Role {
					return nil
				}
			}
			if q.CommitteeIDHex != nil {
				if f.CommitteeID == nil || *f.CommitteeID != *q.CommitteeIDHex {
					return nil
				}
			}
			if q.ValidatorIndex != nil {
				if f.ValidatorIndex == nil || *f.ValidatorIndex != *q.ValidatorIndex {
					return nil
				}
			}
			out = append(out, f)
			return nil
		})
		if err != nil {
			return QueryResult{}, fmt.Errorf("query findings (slot=%d): %w", slot, err)
		}
		if len(out) >= q.Limit {
			break
		}
		// Avoid overflow on slot++ when iterating full uint64 space.
		if slot == q.To {
			break
		}
	}
	return QueryResult{Findings: out}, nil
}

func (s *DBStore) Prune(pruneBefore phase0.Slot) error {
	// Maintain a last-pruned marker to avoid re-pruning the entire range repeatedly.
	lastPruned, err := s.getLastPrunedSlot()
	if err != nil {
		return err
	}
	// If uninitialized, start from the earliest slot we know we stored.
	if lastPruned == 0 {
		if minSlot, ok, err := s.getSlotBound(metaMinSlotKey); err == nil && ok {
			lastPruned = minSlot
		} else {
			// No known data; fast-forward marker to avoid scanning from genesis.
			_ = s.setLastPrunedSlot(pruneBefore)
			_ = s.setSlotBound(metaMinSlotKey, pruneBefore)
			return nil
		}
	}
	if lastPruned >= pruneBefore {
		return nil
	}

	for slot := lastPruned; slot < pruneBefore; slot++ {
		// Delete all findings for the slot.
		prefixFinding := makeSlotPrefix(findingPrefixKey, slot)
		_ = s.db.GetAll(prefixFinding, func(_ int, obj basedb.Obj) error {
			_ = s.db.Delete(prefixFinding, obj.Key)
			return nil
		})
		// Delete all counts for the slot.
		prefixCount := makeSlotPrefix(countPrefixKey, slot)
		_ = s.db.GetAll(prefixCount, func(_ int, obj basedb.Obj) error {
			_ = s.db.Delete(prefixCount, obj.Key)
			return nil
		})

		// Update marker as we go (best effort).
		_ = s.setLastPrunedSlot(slot + 1)
		_ = s.setSlotBound(metaMinSlotKey, slot+1)

		if slot == pruneBefore-1 {
			break
		}
	}
	return nil
}

func (s *DBStore) getLastPrunedSlot() (phase0.Slot, error) {
	prefix := []byte(metaPrefixKey)
	obj, found, err := s.db.Get(prefix, []byte(metaLastPrunedKey))
	if err != nil {
		return 0, fmt.Errorf("get last pruned meta: %w", err)
	}
	if !found {
		return 0, nil
	}
	if len(obj.Value) != 8 {
		return 0, fmt.Errorf("invalid last pruned meta encoding")
	}
	v := binary.LittleEndian.Uint64(obj.Value)
	return phase0.Slot(v), nil
}

func (s *DBStore) setLastPrunedSlot(slot phase0.Slot) error {
	prefix := []byte(metaPrefixKey)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(slot))
	return s.db.Set(prefix, []byte(metaLastPrunedKey), b[:])
}

func (s *DBStore) updateSlotBounds(slot phase0.Slot) error {
	minSlot, minOK, err := s.getSlotBound(metaMinSlotKey)
	if err != nil {
		return err
	}
	maxSlot, maxOK, err := s.getSlotBound(metaMaxSlotKey)
	if err != nil {
		return err
	}
	if !minOK || slot < minSlot {
		_ = s.setSlotBound(metaMinSlotKey, slot)
	}
	if !maxOK || slot > maxSlot {
		_ = s.setSlotBound(metaMaxSlotKey, slot)
	}
	return nil
}

func (s *DBStore) getSlotBound(key string) (phase0.Slot, bool, error) {
	prefix := []byte(metaPrefixKey)
	obj, found, err := s.db.Get(prefix, []byte(key))
	if err != nil {
		return 0, false, fmt.Errorf("get slot bound %s: %w", key, err)
	}
	if !found {
		return 0, false, nil
	}
	if len(obj.Value) != 8 {
		return 0, false, fmt.Errorf("invalid slot bound encoding for %s", key)
	}
	return phase0.Slot(binary.LittleEndian.Uint64(obj.Value)), true, nil
}

func (s *DBStore) setSlotBound(key string, slot phase0.Slot) error {
	prefix := []byte(metaPrefixKey)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(slot))
	return s.db.Set(prefix, []byte(key), b[:])
}

func makeSlotPrefix(base string, slot phase0.Slot) []byte {
	var b [4]byte
	// #nosec G115
	binary.LittleEndian.PutUint32(b[:], uint32(uint64(slot)))
	return append([]byte(base), b[:]...)
}

// reasonToByte provides a stable small key discriminator for a reason code.
// Only reasons in the authoritative set are supported; unknown values return 0.
func reasonToByte(r ReasonCode) byte {
	switch r {
	case ReasonScheduleMissingIndex:
		return 1
	case ReasonScheduleRoleBitMissing:
		return 2
	case ReasonScheduleNotComputed:
		return 3
	case ReasonScheduleComputeFailed:
		return 4
	case ReasonScheduleJobDropped:
		return 5
	case ReasonScheduleBeforeDutiesReady:
		return 6
	case ReasonScheduleReadFailed:
		return 7
	case ReasonDutyFetchFailed:
		return 10
	case ReasonDutyStoreIncomplete:
		return 11
	case ReasonRPCFallbackFailed:
		return 12
	case ReasonRPCFallbackSkipped:
		return 13
	case ReasonRegistryIndexNotFound:
		return 20
	case ReasonCommitteeLinkMissing:
		return 21
	case ReasonCommitteeLinkMismatch:
		return 22
	case ReasonRegistryCommitteeMismatch:
		return 23
	case ReasonLinksReadFailed:
		return 24
	case ReasonUnexpectedWireTrace:
		return 30
	case ReasonRoleClassificationSuspect:
		return 31
	case ReasonTraceSlotMisattributed:
		return 32
	default:
		return 0
	}
}

func (s *DBStore) CountSlot(slot phase0.Slot) (int64, error) {
	prefixFinding := makeSlotPrefix(findingPrefixKey, slot)
	return s.db.CountPrefix(prefixFinding)
}
