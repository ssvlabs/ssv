package ssv

import (
	"maps"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// requestAuthRetentionSlots bounds how long reconstructed request auths outlive their proposal
// slot. An auth is written up to the proposer lookahead ahead of its slot and consumed at (or just
// before) it — §4 bid requests and the epoch-prior submitBuilderPreferences — so entries for slots
// this far behind the newest write are dead weight.
const requestAuthRetentionSlots = 4

// RequestAuthCache holds, per (validator, proposal slot), the threshold-reconstructed
// SignedRequestAuthV1 for each configured builder relationship (issue #2962 B1), keyed by
// gloas.BuilderIdentity. The §5 slot sub-runners write on reconstruction quorum; the §4 produce
// path (and later the ahead-of-time submitBuilderPreferences) read. It is shared between a single
// validator's runners; lives in package ssv alongside ProposedBlockRoots for the same
// import-cycle reason. Safe for concurrent use.
type RequestAuthCache struct {
	mu    sync.Mutex
	auths map[phase0.ValidatorIndex]map[phase0.Slot]map[string]*gloas.SignedRequestAuthV1
}

func NewRequestAuthCache() *RequestAuthCache {
	return &RequestAuthCache{auths: make(map[phase0.ValidatorIndex]map[phase0.Slot]map[string]*gloas.SignedRequestAuthV1)}
}

// Store records the reconstructed auth for the validator's proposal slot under the builder
// identity, and evicts slots more than the retention window behind the newest stored slot.
func (c *RequestAuthCache) Store(validatorIndex phase0.ValidatorIndex, slot phase0.Slot, builderIdentity string, auth *gloas.SignedRequestAuthV1) {
	c.mu.Lock()
	defer c.mu.Unlock()

	bySlot := c.auths[validatorIndex]
	if bySlot == nil {
		bySlot = make(map[phase0.Slot]map[string]*gloas.SignedRequestAuthV1)
		c.auths[validatorIndex] = bySlot
	}
	byBuilder := bySlot[slot]
	if byBuilder == nil {
		byBuilder = make(map[string]*gloas.SignedRequestAuthV1)
		bySlot[slot] = byBuilder
	}
	byBuilder[builderIdentity] = auth

	for sl := range bySlot {
		if slot > requestAuthRetentionSlots && sl < slot-requestAuthRetentionSlots {
			delete(bySlot, sl)
		}
	}
}

// Get returns a copy of the builder-identity → reconstructed-auth map for the validator's proposal
// slot; empty when nothing reconstructed yet.
func (c *RequestAuthCache) Get(validatorIndex phase0.ValidatorIndex, slot phase0.Slot) map[string]*gloas.SignedRequestAuthV1 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.auths[validatorIndex][slot])
}
