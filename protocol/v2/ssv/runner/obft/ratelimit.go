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
//   - Onion:        ≤ K per slot — KindOnion may be emitted multiple times
//     per (slot, op) as σ-eligibility transitions late; cap at K (one per
//     potential layer transition).
//   - NR:           ≤ 1 per slot — KindNR is emitted at most once per
//     (slot, op) at end-of-Phase-2 force-commit.
//   - Certificate:  ≤ 1 per slot — final-certificate gossip is one-shot.
//
// Forget(slot) releases per-slot tracking memory; the runner calls it when
// ending an instance.
type RateLimiter struct {
	mu sync.Mutex

	// bundleSeen[(slot, op, layer)] tracks Phase-1 bundle observations.
	bundleSeen map[layerOpKey]struct{}
	// onionCount[(slot, op)] counts cumulative KindOnion observations.
	onionCount map[onionKey]int
	// nrSeen[(slot, op)] tracks KindNR observations.
	nrSeen map[onionKey]struct{}
	// certSeen[(slot, op)] tracks KindCertificate observations.
	certSeen map[onionKey]struct{}
}

type onionKey struct {
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
		onionCount: make(map[onionKey]int),
		nrSeen:     make(map[onionKey]struct{}),
		certSeen:   make(map[onionKey]struct{}),
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

// AllowOnion records one KindOnion observation for (slot, op) and returns
// nil. Returns an error if the cumulative count would exceed K.
func (r *RateLimiter) AllowOnion(slot phase0.Slot, op spectypes.OperatorID, K int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := onionKey{slot, op}
	if r.onionCount[k] >= K {
		return fmt.Errorf("obft adapter: rate limit: operator %d emitted %d KindOnion messages for slot %d (max %d)",
			op, r.onionCount[k], slot, K)
	}
	r.onionCount[k]++
	return nil
}

// AllowNR records the operator's KindNR for `slot` and returns nil.
// Returns an error on duplicate.
func (r *RateLimiter) AllowNR(slot phase0.Slot, op spectypes.OperatorID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := onionKey{slot, op}
	if _, exists := r.nrSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: operator %d already submitted a KindNR for slot %d", op, slot)
	}
	r.nrSeen[k] = struct{}{}
	return nil
}

// AllowCertificate records the operator's KindCertificate for `slot`.
// Returns an error on duplicate.
func (r *RateLimiter) AllowCertificate(slot phase0.Slot, op spectypes.OperatorID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := onionKey{slot, op}
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
	for k := range r.onionCount {
		if k.slot == slot {
			delete(r.onionCount, k)
		}
	}
	for k := range r.nrSeen {
		if k.slot == slot {
			delete(r.nrSeen, k)
		}
	}
	for k := range r.certSeen {
		if k.slot == slot {
			delete(r.certSeen, k)
		}
	}
}
