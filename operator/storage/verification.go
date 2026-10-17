package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// Background-verification state (see the verifier in eth/eventsyncer):
//   - unverifiedRangePrefix journals block ranges synced optimistically (getLogs only, no
//     completeness check) that await verification against chain data.
//   - blockLogIDsPrefix stores, per block, the IDs of the contract logs the optimistic sync
//     received there; the verifier compares them with what it fetches and, if they differ, with
//     the block's receipts.
//   - unsettledBlockPrefix marks blocks no source could authoritatively verify yet (a bloom match
//     without logs or usable receipts, or a disagreement receipts couldn't settle); the verifier
//     re-checks them until they settle.
//   - resyncRequiredKey, when set, tells the node at startup to drop registry state and
//     resync from scratch with inline verification (the verifier sets it on a detected miss).
//   - resyncInProgressKey, when set, means a resync already dropped state and is rebuilding, so
//     an interrupted repair resumes from the last-processed marker instead of dropping again.
//   - lastResyncKey stores when a resync was last flagged, to rate-limit auto-repair.
var (
	unverifiedRangePrefix = []byte("operator/unverified-range/")
	blockLogIDsPrefix     = []byte("operator/block-log-ids/")
	unsettledBlockPrefix  = []byte("operator/unsettled-block/")
	resyncRequiredKey     = []byte("resync-required")
	resyncInProgressKey   = []byte("resync-in-progress")
	lastResyncKey         = []byte("last-resync-unix")
)

// UnverifiedRange is a journalled block range awaiting background verification. Cursor is the
// next block to verify; the range is fully verified (and removed) once Cursor exceeds To.
type UnverifiedRange struct {
	From   uint64 `json:"from"`
	To     uint64 `json:"to"`
	Cursor uint64 `json:"cursor"`
}

func beUint64(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

// SaveUnverifiedRange upserts a range (keyed by From), used both to enqueue a range and to
// persist verification cursor progress.
func (s *storage) SaveUnverifiedRange(rw basedb.ReadWriter, r UnverifiedRange) error {
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return s.db.Using(rw).Set(unverifiedRangePrefix, beUint64(r.From), b)
}

// ListUnverifiedRanges returns all ranges awaiting verification, ordered by From block.
func (s *storage) ListUnverifiedRanges(r basedb.Reader) ([]UnverifiedRange, error) {
	var ranges []UnverifiedRange
	err := s.db.UsingReader(r).GetAll(unverifiedRangePrefix, func(_ int, obj basedb.Obj) error {
		var ur UnverifiedRange
		if err := json.Unmarshal(obj.Value, &ur); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		ranges = append(ranges, ur)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	return ranges, nil
}

// DeleteUnverifiedRange removes a fully-verified range from the journal.
func (s *storage) DeleteUnverifiedRange(rw basedb.ReadWriter, from uint64) error {
	return s.db.Using(rw).Delete(unverifiedRangePrefix, beUint64(from))
}

// SaveBlockLogIDs records the encoded IDs of the contract logs received for a block during an
// optimistic sync (written in the same transaction that advances the last-processed marker).
func (s *storage) SaveBlockLogIDs(rw basedb.ReadWriter, block uint64, ids []byte) error {
	return s.db.Using(rw).Set(blockLogIDsPrefix, beUint64(block), ids)
}

// GetBlockLogIDs returns the recorded log IDs for a block, or found=false if none (which the
// verifier treats as "the optimistic sync saw no logs there").
func (s *storage) GetBlockLogIDs(r basedb.Reader, block uint64) ([]byte, bool, error) {
	obj, found, err := s.db.UsingReader(r).Get(blockLogIDsPrefix, beUint64(block))
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	return obj.Value, true, nil
}

// DeleteBlockLogIDs removes a block's recorded log IDs once the verifier has settled it.
func (s *storage) DeleteBlockLogIDs(rw basedb.ReadWriter, block uint64) error {
	return s.db.Using(rw).Delete(blockLogIDsPrefix, beUint64(block))
}

// SaveUnsettledBlock marks a block as not yet authoritatively verified.
func (s *storage) SaveUnsettledBlock(rw basedb.ReadWriter, block uint64) error {
	return s.db.Using(rw).Set(unsettledBlockPrefix, beUint64(block), []byte{1})
}

// ListUnsettledBlocks returns the blocks awaiting authoritative verification, in ascending order.
func (s *storage) ListUnsettledBlocks(r basedb.Reader) ([]uint64, error) {
	var blocks []uint64
	err := s.db.UsingReader(r).GetAll(unsettledBlockPrefix, func(_ int, obj basedb.Obj) error {
		if len(obj.Key) != 8 {
			return fmt.Errorf("malformed unsettled-block key %x", obj.Key)
		}
		blocks = append(blocks, binary.BigEndian.Uint64(obj.Key))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	return blocks, nil
}

// DeleteUnsettledBlock clears a block's unsettled mark once it's verified.
func (s *storage) DeleteUnsettledBlock(rw basedb.ReadWriter, block uint64) error {
	return s.db.Using(rw).Delete(unsettledBlockPrefix, beUint64(block))
}

// SetResyncRequired marks that the node must drop registry state and resync from scratch
// (with inline verification) on its next start.
func (s *storage) SetResyncRequired(rw basedb.ReadWriter) error {
	return s.db.Using(rw).Set(OperatorStoragePrefix, resyncRequiredKey, []byte{1})
}

// IsResyncRequired reports whether a full resync has been requested.
func (s *storage) IsResyncRequired(r basedb.Reader) (bool, error) {
	_, found, err := s.db.UsingReader(r).Get(OperatorStoragePrefix, resyncRequiredKey)
	if err != nil {
		return false, err
	}
	return found, nil
}

// DropVerificationJournal clears the background-verification journal: pending ranges, recorded
// log IDs and unsettled blocks. It deliberately leaves the resync flags untouched — the resync
// repair drops the journal before rebuilding, but keeps its flags until the verified resync
// completes.
func (s *storage) DropVerificationJournal() error {
	if err := s.db.DropPrefix(unverifiedRangePrefix); err != nil {
		return fmt.Errorf("drop unverified ranges: %w", err)
	}
	if err := s.db.DropPrefix(blockLogIDsPrefix); err != nil {
		return fmt.Errorf("drop block log IDs: %w", err)
	}
	if err := s.db.DropPrefix(unsettledBlockPrefix); err != nil {
		return fmt.Errorf("drop unsettled blocks: %w", err)
	}
	return nil
}

// ClearResyncRequired clears the resync-required flag, called once a verified resync has
// completed so the next start is a normal boot.
func (s *storage) ClearResyncRequired(rw basedb.ReadWriter) error {
	return s.db.Using(rw).Delete(OperatorStoragePrefix, resyncRequiredKey)
}

// SetResyncInProgress marks that a resync has dropped registry state and is rebuilding it, so an
// interrupted repair resumes from the last-processed marker instead of dropping again.
func (s *storage) SetResyncInProgress(rw basedb.ReadWriter) error {
	return s.db.Using(rw).Set(OperatorStoragePrefix, resyncInProgressKey, []byte{1})
}

// IsResyncInProgress reports whether a resync has already dropped state and is mid-rebuild.
func (s *storage) IsResyncInProgress(r basedb.Reader) (bool, error) {
	_, found, err := s.db.UsingReader(r).Get(OperatorStoragePrefix, resyncInProgressKey)
	if err != nil {
		return false, err
	}
	return found, nil
}

// ClearResyncInProgress clears the resync-in-progress flag once the verified resync completes.
func (s *storage) ClearResyncInProgress(rw basedb.ReadWriter) error {
	return s.db.Using(rw).Delete(OperatorStoragePrefix, resyncInProgressKey)
}

// SetLastResyncTime records when a resync was last flagged, used to rate-limit auto-repair.
func (s *storage) SetLastResyncTime(rw basedb.ReadWriter, t time.Time) error {
	// #nosec G115 -- unix seconds fit in uint64 well past any realistic time
	return s.db.Using(rw).Set(OperatorStoragePrefix, lastResyncKey, beUint64(uint64(t.Unix())))
}

// GetLastResyncTime returns when a resync was last flagged, or found=false if never.
func (s *storage) GetLastResyncTime(r basedb.Reader) (time.Time, bool, error) {
	obj, found, err := s.db.UsingReader(r).Get(OperatorStoragePrefix, lastResyncKey)
	if err != nil {
		return time.Time{}, false, err
	}
	if !found {
		return time.Time{}, false, nil
	}
	// #nosec G115 -- value was written from a unix timestamp
	return time.Unix(int64(binary.BigEndian.Uint64(obj.Value)), 0), true, nil
}
