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
//   - blockLogDigestPrefix stores, per block, a digest of the contract logs the optimistic
//     sync received there; the verifier recomputes it from receipts and compares.
//   - resyncRequiredKey, when set, tells the node at startup to drop registry state and
//     resync from scratch with inline verification (the verifier sets it on a detected miss).
//   - resyncInProgressKey, when set, means a resync already dropped state and is rebuilding, so
//     an interrupted repair resumes from the last-processed marker instead of dropping again.
//   - lastResyncKey stores when a resync was last flagged, to rate-limit auto-repair.
var (
	unverifiedRangePrefix = []byte("operator/unverified-range/")
	blockLogDigestPrefix  = []byte("operator/block-log-digest/")
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

// SaveBlockLogDigest records the digest of the contract logs received for a block during an
// optimistic sync (written in the same transaction that advances the last-processed marker).
func (s *storage) SaveBlockLogDigest(rw basedb.ReadWriter, block uint64, digest []byte) error {
	return s.db.Using(rw).Set(blockLogDigestPrefix, beUint64(block), digest)
}

// GetBlockLogDigest returns the recorded digest for a block, or found=false if none (which
// the verifier treats as "the optimistic sync saw no logs there").
func (s *storage) GetBlockLogDigest(r basedb.Reader, block uint64) ([]byte, bool, error) {
	obj, found, err := s.db.UsingReader(r).Get(blockLogDigestPrefix, beUint64(block))
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	return obj.Value, true, nil
}

// DeleteBlockLogDigest removes a block's digest once the verifier has checked it.
func (s *storage) DeleteBlockLogDigest(rw basedb.ReadWriter, block uint64) error {
	return s.db.Using(rw).Delete(blockLogDigestPrefix, beUint64(block))
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

// DropVerificationJournal clears the background-verification journal: pending ranges and block
// digests. It deliberately leaves the resync flags untouched — the resync repair drops the
// journal before rebuilding, but keeps its flags until the verified resync completes.
func (s *storage) DropVerificationJournal() error {
	if err := s.db.DropPrefix(unverifiedRangePrefix); err != nil {
		return fmt.Errorf("drop unverified ranges: %w", err)
	}
	if err := s.db.DropPrefix(blockLogDigestPrefix); err != nil {
		return fmt.Errorf("drop block-log digests: %w", err)
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
