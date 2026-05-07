package validation

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// obftValidationMaxDistinctPerOpSlot caps how many distinct envelope-bodies
// the validation layer admits per (msgID, slot, op, kind) bucket. Mirrors
// the runner-side rate limiter's MaxDistinctPerOpSlot — enforcing the cap
// pre-BLS saves the BLS verify cost on rejected envelopes (a byzantine
// who already exhausted the bucket can no longer spend our CPU).
const obftValidationMaxDistinctPerOpSlot = 8

// obftAdmissionMaxAge bounds the per-entry retention window. Sized for
// OBFT's in-slot completion plus a small forward budget — anything older
// is rejected upstream by the slot-window check, anything beyond the
// forward budget is rejected likewise. 8 slots ≈ 96s on mainnet covers
// the [-2, +4] window with comfortable slack.
const obftAdmissionMaxAge = 8 * 12 * time.Second

// obftAdmissionKey identifies a single distinct envelope-body within the
// per-(msgID, slot, op, kind) bucket. msgID isolates validators (the spec
// MessageID encodes domain + role + validator-pubkey for proposer-OBFT).
type obftAdmissionKey struct {
	msgID spectypes.MessageID
	slot  phase0.Slot
	op    spectypes.OperatorID
	kind  byte
	hash  [32]byte
}

// obftAdmissionBucket is the cap-tracking key (without hash) for a single
// (msgID, slot, op, kind) admissions bucket.
type obftAdmissionBucket struct {
	msgID spectypes.MessageID
	slot  phase0.Slot
	op    spectypes.OperatorID
	kind  byte
}

// obftAdmissionTracker enforces a per-(msgID, slot, op, kind) bucket cap
// at the validation layer, identical to the runner-side rate limiter's
// shape but applied BEFORE BLS verification. Without this, a byzantine
// can keep paying validation-layer BLS cost up to the runner-side cap
// (~MaxDistinctPerOpSlot per slot per op).
type obftAdmissionTracker struct {
	mu sync.Mutex

	seen   map[obftAdmissionKey]time.Time
	counts map[obftAdmissionBucket]int

	maxAge       time.Duration
	now          func() time.Time
	lastEviction time.Time
}

func newOBFTAdmissionTracker() *obftAdmissionTracker {
	return &obftAdmissionTracker{
		seen:   make(map[obftAdmissionKey]time.Time),
		counts: make(map[obftAdmissionBucket]int),
		maxAge: obftAdmissionMaxAge,
		now:    time.Now,
	}
}

// Admit returns nil if `body` is admissible, an error if the same body has
// been seen for this (msgID, slot, op, kind) before OR if the bucket is
// already at the distinct-content cap.
func (t *obftAdmissionTracker) Admit(
	msgID spectypes.MessageID,
	slot phase0.Slot,
	op spectypes.OperatorID,
	kind byte,
	body []byte,
) error {
	hash := sha256.Sum256(body)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evictExpiredLocked()

	k := obftAdmissionKey{msgID: msgID, slot: slot, op: op, kind: kind, hash: hash}
	if _, exists := t.seen[k]; exists {
		return fmt.Errorf("OBFT envelope: identical content from operator %d at slot %d kind %d (already admitted)",
			op, slot, kind)
	}

	bucket := obftAdmissionBucket{msgID: msgID, slot: slot, op: op, kind: kind}
	if t.counts[bucket] >= obftValidationMaxDistinctPerOpSlot {
		return fmt.Errorf("OBFT envelope: too many distinct messages from operator %d at slot %d kind %d (cap %d)",
			op, slot, kind, obftValidationMaxDistinctPerOpSlot)
	}

	t.seen[k] = t.now()
	t.counts[bucket]++
	return nil
}

// evictExpiredLocked sweeps entries past maxAge. Throttled to (maxAge / 8)
// so sustained Admit traffic doesn't pay full O(n) on every call. Counters
// decrement in lockstep so capacity recovers as buckets age out.
func (t *obftAdmissionTracker) evictExpiredLocked() {
	now := t.now()
	if !t.lastEviction.IsZero() && now.Sub(t.lastEviction) < t.maxAge/8 {
		return
	}
	t.lastEviction = now
	cutoff := now.Add(-t.maxAge)
	for k, ts := range t.seen {
		if ts.Before(cutoff) {
			delete(t.seen, k)
			b := obftAdmissionBucket{msgID: k.msgID, slot: k.slot, op: k.op, kind: k.kind}
			if t.counts[b] > 1 {
				t.counts[b]--
			} else {
				delete(t.counts, b)
			}
		}
	}
}
