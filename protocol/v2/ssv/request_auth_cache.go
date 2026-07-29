package ssv

import (
	"maps"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// RequestAuthCache holds, per proposal slot, the threshold-reconstructed SignedRequestAuthV1 for
// each configured builder relationship (issue #2962 B1), keyed by gloas.BuilderIdentity. The §5
// slot sub-runners write on reconstruction quorum; the §4 produce path (and later the ahead-of-time
// submitBuilderPreferences) will read. One instance per validator, shared between its runners like
// the sibling ProposedBlockRoots and in package ssv for the same import-cycle reason. Safe for
// concurrent use.
type RequestAuthCache struct {
	// currentSlot anchors eviction: writes land up to a proposer lookahead ahead of their slot, so
	// pruning by the clock — not by the last-written slot — is what keeps one future slot's auths
	// from evicting another's.
	currentSlot func() phase0.Slot

	mu    sync.Mutex
	auths map[phase0.Slot]map[string]*gloas.SignedRequestAuthV1
}

func NewRequestAuthCache(currentSlot func() phase0.Slot) *RequestAuthCache {
	return &RequestAuthCache{
		currentSlot: currentSlot,
		auths:       make(map[phase0.Slot]map[string]*gloas.SignedRequestAuthV1),
	}
}

// Store records the reconstructed auth for the proposal slot under the builder identity, and
// evicts slots the chain has moved past. Auths for future proposal slots are always kept — their
// count is bounded by the proposer lookahead times the builder-entry cap.
func (c *RequestAuthCache) Store(slot phase0.Slot, builderIdentity string, auth *gloas.SignedRequestAuthV1) {
	c.mu.Lock()
	defer c.mu.Unlock()

	byBuilder := c.auths[slot]
	if byBuilder == nil {
		byBuilder = make(map[string]*gloas.SignedRequestAuthV1)
		c.auths[slot] = byBuilder
	}
	byBuilder[builderIdentity] = auth

	current := c.currentSlot()
	for sl := range c.auths {
		if sl < current {
			delete(c.auths, sl)
		}
	}
}

// Get returns a copy of the builder-identity → reconstructed-auth map for the proposal slot; empty
// when nothing reconstructed yet.
func (c *RequestAuthCache) Get(slot phase0.Slot) map[string]*gloas.SignedRequestAuthV1 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.auths[slot])
}
