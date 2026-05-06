package obft

import (
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

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
// Forget(slot) releases per-slot tracking memory; the runner calls it when
// ending an instance.
type RateLimiter struct {
	mu sync.Mutex

	// bundleSeen[(slot, op, layer)] tracks Phase-1 bundle observations.
	bundleSeen map[layerOpKey]struct{}
	// commitSeen[(slot, op)] tracks KindCommit observations.
	commitSeen map[opKey]struct{}
	// certSeen[(slot, op)] tracks KindCertificate observations.
	certSeen map[opKey]struct{}
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

// NewRateLimiter creates a fresh limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		bundleSeen: make(map[layerOpKey]struct{}),
		commitSeen: make(map[opKey]struct{}),
		certSeen:   make(map[opKey]struct{}),
	}
}

// AllowPhase1Bundle records the operator's Phase-1 bundle for (slot, layer)
// and returns nil. Returns an error if a duplicate is detected.
func (r *RateLimiter) AllowPhase1Bundle(slot phase0.Slot, op spectypes.OperatorID, layer int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := layerOpKey{slot, op, layer}
	if _, exists := r.bundleSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: operator %d already submitted a phase-1 bundle for slot %d layer %d",
			op, slot, layer)
	}
	r.bundleSeen[k] = struct{}{}
	return nil
}

// AllowCommit records the operator's KindCommit for `slot` and returns nil.
// Returns an error on duplicate.
func (r *RateLimiter) AllowCommit(slot phase0.Slot, op spectypes.OperatorID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := opKey{slot, op}
	if _, exists := r.commitSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: operator %d already submitted a KindCommit for slot %d", op, slot)
	}
	r.commitSeen[k] = struct{}{}
	return nil
}

// AllowCertificate records the operator's KindCertificate for `slot`.
// Returns an error on duplicate.
func (r *RateLimiter) AllowCertificate(slot phase0.Slot, op spectypes.OperatorID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := opKey{slot, op}
	if _, exists := r.certSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: operator %d already submitted a KindCertificate for slot %d", op, slot)
	}
	r.certSeen[k] = struct{}{}
	return nil
}

// Forget releases per-slot tracking memory. Idempotent.
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
