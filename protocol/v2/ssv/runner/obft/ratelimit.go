package obft

import (
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// DefaultMaxAge is the per-entry TTL for rate-limiter records: 32 slots
// (~1 epoch on Ethereum mainnet at 12s slots = 6.4 min). Longer than any
// realistic OBFT instance lifetime; short enough to bound memory under
// degenerate runner-crash scenarios.
const DefaultMaxAge = 32 * 12 * time.Second

// RateLimiter enforces protocol-allowed message counts per (slot, operator,
// kind, layer) tuple. It exists at the message-validation boundary as
// defense-in-depth on top of Instance's content-dedup; rejecting duplicates
// here saves wasted decode/verify work and surfaces protocol violations as
// explicit errors.
//
// Allowed counts per (slot, operator):
//
//   - Phase1Bundle: ≤ 1 per (slot, layer) — only one Phase-1 bundle per
//     leader per layer (the leader's σ_V is locked once per (slot, layer)
//     by EKM enforcement).
//   - Commit:       ≤ 1 per slot — KindCommit is emitted exactly once per
//     (slot, op) at T_commit per spec §Phase 2.
//   - Certificate:  ≤ 1 per slot — final-certificate gossip is one-shot.
//
// Memory management: each recorded entry carries a creation timestamp and
// is lazy-evicted by any Allow* call once older than MaxAge. Forget(slot)
// remains as an explicit cleanup hook the runner calls on instance
// completion; with TTL-based eviction it is no longer load-bearing for
// memory safety.
type RateLimiter struct {
	mu sync.Mutex

	bundleSeen map[layerOpKey]time.Time
	commitSeen map[opKey]time.Time
	certSeen   map[opKey]time.Time

	maxAge time.Duration
	now    func() time.Time
}

type opKey struct {
	slot phase0.Slot
	op   spectypes.OperatorID
}

type layerOpKey struct {
	slot  phase0.Slot
	op    spectypes.OperatorID
	layer int
}

// NewRateLimiter creates a fresh limiter with the default MaxAge.
func NewRateLimiter() *RateLimiter {
	return NewRateLimiterWithMaxAge(DefaultMaxAge)
}

// NewRateLimiterWithMaxAge creates a fresh limiter with a custom retention
// window. Use the default in production; override only for tests.
func NewRateLimiterWithMaxAge(maxAge time.Duration) *RateLimiter {
	return &RateLimiter{
		bundleSeen: make(map[layerOpKey]time.Time),
		commitSeen: make(map[opKey]time.Time),
		certSeen:   make(map[opKey]time.Time),
		maxAge:     maxAge,
		now:        time.Now,
	}
}

// AllowPhase1Bundle records the operator's Phase-1 bundle for (slot, layer)
// and returns nil. Returns an error if a duplicate is detected.
func (r *RateLimiter) AllowPhase1Bundle(slot phase0.Slot, op spectypes.OperatorID, layer int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	k := layerOpKey{slot, op, layer}
	if _, exists := r.bundleSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: operator %d already submitted a phase-1 bundle for slot %d layer %d",
			op, slot, layer)
	}
	r.bundleSeen[k] = r.now()
	return nil
}

// AllowCommit records the operator's KindCommit for `slot`. Returns an error
// on duplicate.
func (r *RateLimiter) AllowCommit(slot phase0.Slot, op spectypes.OperatorID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	k := opKey{slot, op}
	if _, exists := r.commitSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: operator %d already submitted a KindCommit for slot %d", op, slot)
	}
	r.commitSeen[k] = r.now()
	return nil
}

// AllowCertificate records the operator's KindCertificate for `slot`.
// Returns an error on duplicate.
func (r *RateLimiter) AllowCertificate(slot phase0.Slot, op spectypes.OperatorID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	k := opKey{slot, op}
	if _, exists := r.certSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: operator %d already submitted a KindCertificate for slot %d", op, slot)
	}
	r.certSeen[k] = r.now()
	return nil
}

// Forget releases per-slot tracking memory eagerly. Idempotent. With
// TTL-based eviction this is no longer required for correctness, but the
// runner calls it on instance completion to free memory promptly.
func (r *RateLimiter) Forget(slot phase0.Slot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.bundleSeen {
		if k.slot == slot {
			delete(r.bundleSeen, k)
		}
	}
	for k := range r.commitSeen {
		if k.slot == slot {
			delete(r.commitSeen, k)
		}
	}
	for k := range r.certSeen {
		if k.slot == slot {
			delete(r.certSeen, k)
		}
	}
}

// evictExpiredLocked removes entries older than maxAge. O(n) per call; n
// is bounded by ~maxAge worth of slots × cluster size, ≈ a few thousand
// at the default settings.
func (r *RateLimiter) evictExpiredLocked() {
	cutoff := r.now().Add(-r.maxAge)
	for k, t := range r.bundleSeen {
		if t.Before(cutoff) {
			delete(r.bundleSeen, k)
		}
	}
	for k, t := range r.commitSeen {
		if t.Before(cutoff) {
			delete(r.commitSeen, k)
		}
	}
	for k, t := range r.certSeen {
		if t.Before(cutoff) {
			delete(r.certSeen, k)
		}
	}
}
