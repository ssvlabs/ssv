package tbft

import (
	"errors"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// RateLimiter enforces protocol-allowed message counts per
// (slot, operator, kind, layer) tuple. It exists at the message-validation
// boundary as defense-in-depth on top of `Instance`'s silent
// de-duplication: rejecting duplicates here prevents wasted decode/verify
// work and surfaces protocol violations as explicit errors.
//
// Allowed counts per (slot, operator) under a single Controller / Instance:
//
//   - Onion:        ≤ 1 in total.
//   - NonReceipt:   ≤ 1 per layer in [0, K-1).
//   - Candidate:    ≤ 1 per layer in [0, K). (In practice, only the
//     designated layer leader sends a candidate at all — non-leaders
//     sending candidates is a protocol violation, but enforcing
//     leader-only is the runner's job; the rate limiter just tracks
//     counts.)
//
// `Forget(slot)` should be called when the runner ends an instance, to
// release the per-slot tracking memory.
//
// Thread-safe.
type RateLimiter struct {
	mu sync.Mutex

	// onionsSeen[(slot, op)] = true once we've seen the operator's onion.
	onionsSeen map[onionKey]struct{}
	// nrSeen[(slot, op, layer)] = true once seen.
	nrSeen map[layerOpKey]struct{}
	// candSeen[(slot, op, layer)] = true once seen.
	candSeen map[layerOpKey]struct{}
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

// NewRateLimiter creates a fresh limiter with empty state.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		onionsSeen: make(map[onionKey]struct{}),
		nrSeen:     make(map[layerOpKey]struct{}),
		candSeen:   make(map[layerOpKey]struct{}),
	}
}

// AllowOnion records the operator's onion for `slot` and returns nil.
// Returns an error if the operator has already submitted an onion for
// this slot (duplicate / rate-limit violation).
func (r *RateLimiter) AllowOnion(slot phase0.Slot, op spectypes.OperatorID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := onionKey{slot, op}
	if _, exists := r.onionsSeen[k]; exists {
		return fmt.Errorf("tbft adapter: rate limit: operator %d already submitted an onion for slot %d", op, slot)
	}
	r.onionsSeen[k] = struct{}{}
	return nil
}

// AllowNonReceipt records the operator's non-receipt for `(slot, layer)`
// and returns nil. Returns an error if the operator has already submitted
// a non-receipt for the same `(slot, layer)`.
func (r *RateLimiter) AllowNonReceipt(slot phase0.Slot, op spectypes.OperatorID, layer int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := layerOpKey{slot, op, layer}
	if _, exists := r.nrSeen[k]; exists {
		return fmt.Errorf("tbft adapter: rate limit: operator %d already submitted a non-receipt for slot %d layer %d", op, slot, layer)
	}
	r.nrSeen[k] = struct{}{}
	return nil
}

// AllowCandidate records the operator's candidate broadcast for
// `(slot, layer)` and returns nil. Returns an error if the operator has
// already submitted a candidate for the same `(slot, layer)`.
func (r *RateLimiter) AllowCandidate(slot phase0.Slot, op spectypes.OperatorID, layer int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := layerOpKey{slot, op, layer}
	if _, exists := r.candSeen[k]; exists {
		return fmt.Errorf("tbft adapter: rate limit: operator %d already submitted a candidate for slot %d layer %d", op, slot, layer)
	}
	r.candSeen[k] = struct{}{}
	return nil
}

// Forget releases all per-slot tracking memory for `slot`. Idempotent.
// The runner should call this when ending an instance.
func (r *RateLimiter) Forget(slot phase0.Slot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.onionsSeen {
		if k.slot == slot {
			delete(r.onionsSeen, k)
		}
	}
	for k := range r.nrSeen {
		if k.slot == slot {
			delete(r.nrSeen, k)
		}
	}
	for k := range r.candSeen {
		if k.slot == slot {
			delete(r.candSeen, k)
		}
	}
}

// ErrNotApplicable is returned by Allow when the message kind isn't one
// the rate limiter tracks (defensive — shouldn't happen in normal flow).
var ErrNotApplicable = errors.New("tbft adapter: rate-limit not applicable for this message kind")
