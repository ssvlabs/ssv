package validation

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// consensusValidationMaxDistinctPerOpSlot caps how many distinct envelope-bodies
// the validation layer admits per (msgID, slot, op, kind) bucket. Mirrors
// the runner-side rate limiter's MaxDistinctPerOpSlot — enforcing the cap
// pre-BLS saves the BLS verify cost on rejected envelopes (a byzantine
// who already exhausted the bucket can no longer spend our CPU).
const consensusValidationMaxDistinctPerOpSlot = 8

// consensusAdmissionMaxAge bounds the per-entry retention window. Sized for
// the OBFT-family in-slot completion plus a small forward budget — anything
// older is rejected upstream by the slot-window check, anything beyond the
// forward budget is rejected likewise. 8 slots ≈ 96s on mainnet covers
// the [-2, +4] window with comfortable slack.
const consensusAdmissionMaxAge = 8 * 12 * time.Second

// consensusAdmissionBucket is the cap-tracking key (without hash) for a single
// (msgID, slot, op, kind) admissions bucket.
type consensusAdmissionBucket struct {
	msgID spectypes.MessageID
	slot  phase0.Slot
	op    spectypes.OperatorID
	kind  byte
}

// consensusAdmissionTracker enforces a per-(msgID, slot, op, kind) bucket cap
// at the validation layer, identical to the runner-side rate limiter's
// shape but applied BEFORE BLS verification. Without this, a byzantine
// can keep paying validation-layer BLS cost up to the runner-side cap
// (~MaxDistinctPerOpSlot per slot per op).
//
// Per-bucket entries are tracked in a small slice attached to each bucket
// counter, so eviction on Admit is O(bucket-size = ≤ MaxDistinctPerOpSlot)
// regardless of how big the global tracker grows. A periodic global sweep
// also runs to reclaim buckets that have gone idle (otherwise stale empty
// buckets would never be GC'd).
type consensusAdmissionTracker struct {
	mu sync.Mutex

	// buckets holds the per-bucket admission state. Each bucket carries
	// its own list of (hash, time) entries, capped at
	// consensusValidationMaxDistinctPerOpSlot. This makes per-Admit eviction
	// trivially cheap (only walk the bucket being touched).
	buckets map[consensusAdmissionBucket]*admissionBucketState

	maxAge          time.Duration
	now             func() time.Time
	lastGlobalSweep time.Time
}

// admissionBucketState is the per-bucket entry list. Entries are appended
// in insertion order; eviction walks the slice for entries past maxAge.
// The seen-hash set is rebuilt on the fly from the slice during Admit
// (cheap because the slice is bounded by the cap).
type admissionBucketState struct {
	entries []admissionEntry
}

type admissionEntry struct {
	hash [32]byte
	ts   time.Time
}

func newConsensusAdmissionTracker() *consensusAdmissionTracker {
	return &consensusAdmissionTracker{
		buckets: make(map[consensusAdmissionBucket]*admissionBucketState),
		maxAge:  consensusAdmissionMaxAge,
		now:     time.Now,
	}
}

// Admit returns nil if `body` is admissible, an error if the same body has
// been seen for this (msgID, slot, op, kind) before OR if the bucket is
// already at the distinct-content cap. Per-bucket aged entries are evicted
// on every Admit so the cap reflects only entries within `maxAge`.
func (t *consensusAdmissionTracker) Admit(
	msgID spectypes.MessageID,
	slot phase0.Slot,
	op spectypes.OperatorID,
	kind byte,
	body []byte,
) error {
	hash := sha256.Sum256(body)
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()
	t.maybeGlobalSweepLocked(now)

	bucket := consensusAdmissionBucket{msgID: msgID, slot: slot, op: op, kind: kind}
	state := t.buckets[bucket]
	if state == nil {
		state = &admissionBucketState{}
		t.buckets[bucket] = state
	}

	// Per-bucket eviction: drop entries past maxAge before checking dedup
	// and cap. Bounded to ≤ consensusValidationMaxDistinctPerOpSlot per call.
	cutoff := now.Add(-t.maxAge)
	if len(state.entries) > 0 {
		filtered := state.entries[:0]
		for _, e := range state.entries {
			if !e.ts.Before(cutoff) {
				filtered = append(filtered, e)
			}
		}
		state.entries = filtered
	}

	for _, e := range state.entries {
		if e.hash == hash {
			return fmt.Errorf("consensus admission: identical content from operator %d at slot %d kind %d (already admitted)",
				op, slot, kind)
		}
	}

	if len(state.entries) >= consensusValidationMaxDistinctPerOpSlot {
		return fmt.Errorf("consensus admission: too many distinct messages from operator %d at slot %d kind %d (cap %d)",
			op, slot, kind, consensusValidationMaxDistinctPerOpSlot)
	}

	state.entries = append(state.entries, admissionEntry{hash: hash, ts: now})
	return nil
}

// maybeGlobalSweepLocked walks all buckets and removes any whose every
// entry is past maxAge. Throttled to (maxAge / 8) — under sustained Admit
// traffic this is O(buckets) per ~12 s on mainnet, cheap. Without this,
// stale empty buckets would never be GC'd (per-bucket Admit eviction
// keeps entries bounded but doesn't drop the bucket itself).
func (t *consensusAdmissionTracker) maybeGlobalSweepLocked(now time.Time) {
	if !t.lastGlobalSweep.IsZero() && now.Sub(t.lastGlobalSweep) < t.maxAge/8 {
		return
	}
	t.lastGlobalSweep = now
	cutoff := now.Add(-t.maxAge)
	for k, state := range t.buckets {
		alive := false
		for _, e := range state.entries {
			if !e.ts.Before(cutoff) {
				alive = true
				break
			}
		}
		if !alive {
			delete(t.buckets, k)
		}
	}
}
