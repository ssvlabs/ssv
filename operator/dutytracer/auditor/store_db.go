package auditor

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

	// Order is either "asc" or "desc". Default is "desc".
	Order string
	// Cursor is the exclusive starting point for pagination. Format: "<slot>/<reason>/<seq>".
	Cursor *string
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
	orderAsc  = "asc"
	orderDesc = "desc"
)

const (
	findingPrefixKey = "af"  // per-slot prefix: af + slotBytes
	countPrefixKey   = "afc" // per-slot prefix: afc + slotBytes; key: reasonByte -> uint16 count
	metaPrefixKey    = "afm"
	summaryPrefixKey = "afs"  // per-slot prefix: afs + slotBytes; key: reasonByte -> json summary
	indexVPrefixKey  = "afvi" // per-validator index: afvi + validatorIndex(8)
	indexCPrefixKey  = "afci" // per-committee index: afci + committeeID(32)
	indexRPrefixKey  = "afre" // per-reason index: afre + reasonByte(1)
)

const (
	metaLastPrunedKey = "last_pruned_slot"
	metaMinSlotKey    = "min_slot"
	metaMaxSlotKey    = "max_slot"
)

func NewDBStore(db basedb.Database) *DBStore {
	return &DBStore{db: db, maxPerSlotReason: 10}
}

func (s *DBStore) SlotBounds() (phase0.Slot, phase0.Slot, bool, error) {
	minSlot, minOK, err := s.getSlotBound(metaMinSlotKey)
	if err != nil {
		return 0, 0, false, err
	}
	maxSlot, maxOK, err := s.getSlotBound(metaMaxSlotKey)
	if err != nil {
		return 0, 0, false, err
	}
	if !minOK && !maxOK {
		return 0, 0, false, nil
	}
	return minSlot, maxSlot, true, nil
}

var errStopIteration = errors.New("stop iteration")

func makeValidatorIndexPrefix(index uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], index)
	return append([]byte(indexVPrefixKey), b[:]...)
}

func makeCommitteePrefix(committeeIDHex string) ([]byte, error) {
	raw, err := hex.DecodeString(committeeIDHex)
	if err != nil {
		return nil, fmt.Errorf("decode committee id: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("invalid committee id length: %d", len(raw))
	}
	return append([]byte(indexCPrefixKey), raw...), nil
}

func makeReasonPrefix(reasonByte byte) []byte {
	return append([]byte(indexRPrefixKey), reasonByte)
}

func makeIndexEntryKey(slot phase0.Slot, reasonByte byte, seq byte) []byte {
	var b [6]byte
	// Store slot in descending order using inverted uint32 big-endian.
	// #nosec G115
	binary.BigEndian.PutUint32(b[:4], ^uint32(uint64(slot)))
	b[4] = reasonByte
	b[5] = seq
	return b[:]
}

func decodeIndexEntryKey(key []byte) (slot phase0.Slot, reasonByte byte, seq byte, ok bool) {
	if len(key) != 6 {
		return 0, 0, 0, false
	}
	v := binary.BigEndian.Uint32(key[:4])
	orig := ^v
	return phase0.Slot(orig), key[4], key[5], true
}

type cursorInfo struct {
	slot       phase0.Slot
	reasonByte byte
	seqByte    byte
	ok         bool
}

func parseCursor(cur string) (cursorInfo, error) {
	// Format: "<slot>/<reason>/<seq>"
	var ci cursorInfo
	parts := strings.Split(cur, "/")
	if len(parts) != 3 {
		return ci, fmt.Errorf("invalid cursor format")
	}
	slotU64, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return ci, fmt.Errorf("invalid cursor slot: %w", err)
	}
	reasonStr := parts[1]
	seqU64, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return ci, fmt.Errorf("invalid cursor seq: %w", err)
	}

	rb := reasonToByte(ReasonCode(reasonStr))
	if rb == 0 {
		return ci, fmt.Errorf("invalid cursor reason: %s", reasonStr)
	}
	if seqU64 > 255 {
		return ci, fmt.Errorf("invalid cursor seq: %d", seqU64)
	}
	ci.slot = phase0.Slot(slotU64)
	ci.reasonByte = rb
	ci.seqByte = byte(seqU64)
	ci.ok = true
	return ci, nil
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

	idxKey := makeIndexEntryKey(slot, reasonByte, seq)
	// Reason index (always present).
	if err := s.db.Set(makeReasonPrefix(reasonByte), idxKey, []byte{}); err != nil {
		return PutResult{}, fmt.Errorf("index reason: %w", err)
	}
	// Validator index index (if available).
	if f.ValidatorIndex != nil {
		if err := s.db.Set(makeValidatorIndexPrefix(*f.ValidatorIndex), idxKey, []byte{}); err != nil {
			return PutResult{}, fmt.Errorf("index validator: %w", err)
		}
	}
	// Committee index (if available).
	if f.CommitteeID != nil && *f.CommitteeID != "" {
		prefix, err := makeCommitteePrefix(*f.CommitteeID)
		if err != nil {
			return PutResult{}, fmt.Errorf("index committee: %w", err)
		}
		if err := s.db.Set(prefix, idxKey, []byte{}); err != nil {
			return PutResult{}, fmt.Errorf("index committee: %w", err)
		}
	}

	// Best-effort slot bounds for prune initialization.
	_ = s.updateSlotBounds(slot)
	return res, nil
}

func (s *DBStore) PutSlotSummary(sum *SlotSummary) error {
	if sum == nil {
		return fmt.Errorf("nil summary")
	}
	if sum.CreatedAt.IsZero() {
		sum.CreatedAt = time.Now().UTC()
	}
	if sum.Version == 0 {
		sum.Version = 1
	}
	slot := phase0.Slot(sum.Slot)
	reasonByte := reasonToByte(sum.Reason)
	if reasonByte == 0 {
		return fmt.Errorf("unsupported reason code: %s", sum.Reason)
	}
	prefix := makeSlotPrefix(summaryPrefixKey, slot)
	val, err := json.Marshal(sum)
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	return s.db.Set(prefix, []byte{reasonByte}, val)
}

func (s *DBStore) QuerySlotSummaries(q SummaryQuery) (SummaryResult, error) {
	if q.Limit <= 0 {
		q.Limit = 500
	}
	if q.To < q.From {
		return SummaryResult{}, fmt.Errorf("'to' must be >= 'from'")
	}
	out := make([]*SlotSummary, 0, minInt(q.Limit, 64))

	wantReasonByte := byte(0)
	if q.Reason != nil {
		wantReasonByte = reasonToByte(*q.Reason)
	}

	for slot := q.To; ; slot-- {
		prefix := makeSlotPrefix(summaryPrefixKey, slot)
		err := s.db.GetAll(prefix, func(_ int, obj basedb.Obj) error {
			if len(out) >= q.Limit {
				return errStopIteration
			}
			if wantReasonByte != 0 {
				if len(obj.Key) != 1 || obj.Key[0] != wantReasonByte {
					return nil
				}
			}
			su := new(SlotSummary)
			if err := json.Unmarshal(obj.Value, su); err != nil {
				return nil
			}
			out = append(out, su)
			if len(out) >= q.Limit {
				return errStopIteration
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errStopIteration) {
				break
			}
			return SummaryResult{}, fmt.Errorf("query summaries (slot=%d): %w", slot, err)
		}
		if slot == q.From || slot == 0 {
			break
		}
	}

	return SummaryResult{Summaries: out}, nil
}

func (s *DBStore) Query(q Query) (QueryResult, error) {
	if q.Limit <= 0 {
		q.Limit = 500
	}
	if q.Order == "" {
		q.Order = orderDesc
	} else {
		q.Order = strings.ToLower(strings.TrimSpace(q.Order))
	}
	if q.Order != orderAsc && q.Order != orderDesc {
		return QueryResult{}, fmt.Errorf("invalid order: %s", q.Order)
	}
	if q.To < q.From {
		return QueryResult{}, fmt.Errorf("'to' must be >= 'from'")
	}

	var cur cursorInfo
	if q.Cursor != nil && strings.TrimSpace(*q.Cursor) != "" {
		ci, err := parseCursor(strings.TrimSpace(*q.Cursor))
		if err != nil {
			return QueryResult{}, err
		}
		cur = ci
	}

	// Use secondary indexes for common lookups when we can return newest-first.
	if q.Order == orderDesc {
		switch {
		case q.ValidatorIndex != nil:
			return s.queryByIndex(makeValidatorIndexPrefix(*q.ValidatorIndex), q, cur)
		case q.CommitteeIDHex != nil:
			pfx, err := makeCommitteePrefix(*q.CommitteeIDHex)
			if err != nil {
				return QueryResult{}, err
			}
			return s.queryByIndex(pfx, q, cur)
		case q.Reason != nil:
			rb := reasonToByte(*q.Reason)
			if rb == 0 {
				return QueryResult{}, fmt.Errorf("unsupported reason: %s", *q.Reason)
			}
			return s.queryByIndex(makeReasonPrefix(rb), q, cur)
		}
	}

	return s.queryBySlotScan(q, cur)
}

func (s *DBStore) queryBySlotScan(q Query, cur cursorInfo) (QueryResult, error) {
	out := make([]*Finding, 0, minInt(q.Limit, 128))

	started := !cur.ok
	wantReasonByte := byte(0)
	if q.Reason != nil {
		wantReasonByte = reasonToByte(*q.Reason)
	}

	handleObj := func(slot phase0.Slot, obj basedb.Obj) error {
		if len(out) >= q.Limit {
			return errStopIteration
		}
		if len(obj.Key) < 2 {
			return nil
		}
		reasonByte := obj.Key[0]
		seqByte := obj.Key[1]

		if !started {
			if q.Order == orderDesc {
				if slot > cur.slot {
					return nil
				}
				if slot < cur.slot {
					started = true
				} else {
					if reasonByte < cur.reasonByte || (reasonByte == cur.reasonByte && seqByte <= cur.seqByte) {
						return nil
					}
					started = true
				}
			} else {
				if slot < cur.slot {
					return nil
				}
				if slot > cur.slot {
					started = true
				} else {
					if reasonByte < cur.reasonByte || (reasonByte == cur.reasonByte && seqByte <= cur.seqByte) {
						return nil
					}
					started = true
				}
			}
		}

		if wantReasonByte != 0 && reasonByte != wantReasonByte {
			return nil
		}

		f := new(Finding)
		if err := json.Unmarshal(obj.Value, f); err != nil {
			return nil
		}
		if !matchQueryFilters(q, f) {
			return nil
		}
		out = append(out, f)
		if len(out) >= q.Limit {
			return errStopIteration
		}
		return nil
	}

	switch q.Order {
	case orderAsc:
		for slot := q.From; slot <= q.To; slot++ {
			prefixFinding := makeSlotPrefix(findingPrefixKey, slot)
			err := s.db.GetAll(prefixFinding, func(_ int, obj basedb.Obj) error {
				return handleObj(slot, obj)
			})
			if err != nil {
				if errors.Is(err, errStopIteration) {
					break
				}
				return QueryResult{}, fmt.Errorf("query findings (slot=%d): %w", slot, err)
			}
			if slot == q.To {
				break
			}
		}
	case orderDesc:
		for slot := q.To; ; slot-- {
			prefixFinding := makeSlotPrefix(findingPrefixKey, slot)
			err := s.db.GetAll(prefixFinding, func(_ int, obj basedb.Obj) error {
				return handleObj(slot, obj)
			})
			if err != nil {
				if errors.Is(err, errStopIteration) {
					break
				}
				return QueryResult{}, fmt.Errorf("query findings (slot=%d): %w", slot, err)
			}
			if slot == q.From || slot == 0 {
				break
			}
		}
	}

	return QueryResult{Findings: out}, nil
}

func (s *DBStore) queryByIndex(prefix []byte, q Query, cur cursorInfo) (QueryResult, error) {
	out := make([]*Finding, 0, minInt(q.Limit, 128))
	started := !cur.ok

	err := s.db.GetAll(prefix, func(_ int, obj basedb.Obj) error {
		if len(out) >= q.Limit {
			return errStopIteration
		}
		slot, reasonByte, seqByte, ok := decodeIndexEntryKey(obj.Key)
		if !ok {
			return nil
		}
		// Newest-first order, so once we pass below q.From we can stop.
		if slot < q.From {
			return errStopIteration
		}
		if slot > q.To {
			return nil
		}

		if !started {
			if slot > cur.slot {
				return nil
			}
			if slot < cur.slot {
				started = true
			} else {
				if reasonByte < cur.reasonByte || (reasonByte == cur.reasonByte && seqByte <= cur.seqByte) {
					return nil
				}
				started = true
			}
		}

		f, err := s.getFinding(slot, reasonByte, seqByte)
		if err != nil || f == nil {
			return nil
		}
		if !matchQueryFilters(q, f) {
			return nil
		}
		out = append(out, f)
		if len(out) >= q.Limit {
			return errStopIteration
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopIteration) {
		return QueryResult{}, err
	}

	return QueryResult{Findings: out}, nil
}

func (s *DBStore) getFinding(slot phase0.Slot, reasonByte byte, seqByte byte) (*Finding, error) {
	prefixFinding := makeSlotPrefix(findingPrefixKey, slot)
	obj, found, err := s.db.Get(prefixFinding, []byte{reasonByte, seqByte})
	if err != nil || !found {
		return nil, err
	}
	f := new(Finding)
	if err := json.Unmarshal(obj.Value, f); err != nil {
		return nil, err
	}
	return f, nil
}

func matchQueryFilters(q Query, f *Finding) bool {
	if f == nil {
		return false
	}
	if q.Reason != nil && f.Reason != *q.Reason {
		return false
	}
	if q.Role != nil {
		if f.Role == nil || *f.Role != *q.Role {
			return false
		}
	}
	if q.CommitteeIDHex != nil {
		if f.CommitteeID == nil || *f.CommitteeID != *q.CommitteeIDHex {
			return false
		}
	}
	if q.ValidatorIndex != nil {
		if f.ValidatorIndex == nil || *f.ValidatorIndex != *q.ValidatorIndex {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
			if len(obj.Key) >= 2 {
				reasonByte := obj.Key[0]
				seqByte := obj.Key[1]
				idxKey := makeIndexEntryKey(slot, reasonByte, seqByte)
				f := new(Finding)
				if err := json.Unmarshal(obj.Value, f); err == nil {
					_ = s.deleteIndexes(f, idxKey, reasonByte)
				}
			}
			_ = s.db.Delete(prefixFinding, obj.Key)
			return nil
		})
		// Delete all counts for the slot.
		prefixCount := makeSlotPrefix(countPrefixKey, slot)
		_ = s.db.GetAll(prefixCount, func(_ int, obj basedb.Obj) error {
			_ = s.db.Delete(prefixCount, obj.Key)
			return nil
		})
		// Delete all summaries for the slot.
		prefixSummary := makeSlotPrefix(summaryPrefixKey, slot)
		_ = s.db.GetAll(prefixSummary, func(_ int, obj basedb.Obj) error {
			_ = s.db.Delete(prefixSummary, obj.Key)
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

func (s *DBStore) deleteIndexes(f *Finding, idxKey []byte, reasonByte byte) error {
	_ = s.db.Delete(makeReasonPrefix(reasonByte), idxKey)
	if f == nil {
		return nil
	}
	if f.ValidatorIndex != nil {
		_ = s.db.Delete(makeValidatorIndexPrefix(*f.ValidatorIndex), idxKey)
	}
	if f.CommitteeID != nil && *f.CommitteeID != "" {
		prefix, err := makeCommitteePrefix(*f.CommitteeID)
		if err == nil {
			_ = s.db.Delete(prefix, idxKey)
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
