package builderendpoint

import (
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type prefetchParentHashCompareResult string

const (
	prefetchParentHashCompareMissing  prefetchParentHashCompareResult = "missing"
	prefetchParentHashCompareMatch    prefetchParentHashCompareResult = "match"
	prefetchParentHashCompareMismatch prefetchParentHashCompareResult = "mismatch"
)

type prefetchParentHashKey struct {
	slot   phase0.Slot
	pubkey phase0.BLSPubKey
}

type prefetchParentHashRecord struct {
	parentHash phase0.Hash32
	expiresAt  time.Time
}

// prefetchParentHashTracker keeps the most recent prefetched parent_hash per (slot, pubkey)
// to enable precise mismatch accounting when get_header is later called.
type prefetchParentHashTracker struct {
	ttl time.Duration
	now func() time.Time

	mu sync.Mutex
	m  map[prefetchParentHashKey]prefetchParentHashRecord
}

func newPrefetchParentHashTracker(ttl time.Duration) *prefetchParentHashTracker {
	if ttl <= 0 {
		ttl = 12 * time.Second
	}
	return &prefetchParentHashTracker{
		ttl: ttl,
		now: time.Now,
		m:   make(map[prefetchParentHashKey]prefetchParentHashRecord),
	}
}

func (t *prefetchParentHashTracker) Record(slot phase0.Slot, pubkey phase0.BLSPubKey, parentHash phase0.Hash32) {
	if t == nil {
		return
	}
	now := t.now()
	rec := prefetchParentHashRecord{
		parentHash: parentHash,
		expiresAt:  now.Add(t.ttl),
	}

	t.mu.Lock()
	t.m[prefetchParentHashKey{slot: slot, pubkey: pubkey}] = rec
	t.mu.Unlock()
}

func (t *prefetchParentHashTracker) Compare(slot phase0.Slot, pubkey phase0.BLSPubKey, parentHash phase0.Hash32) prefetchParentHashCompareResult {
	if t == nil {
		return prefetchParentHashCompareMissing
	}

	k := prefetchParentHashKey{slot: slot, pubkey: pubkey}

	t.mu.Lock()
	rec, ok := t.m[k]
	if ok && !rec.expiresAt.IsZero() && t.now().After(rec.expiresAt) {
		delete(t.m, k)
		ok = false
	}
	t.mu.Unlock()

	if !ok {
		return prefetchParentHashCompareMissing
	}
	if rec.parentHash == parentHash {
		return prefetchParentHashCompareMatch
	}
	return prefetchParentHashCompareMismatch
}
