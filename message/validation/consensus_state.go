package validation

import (
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/emirpasic/gods/maps/treemap"
	"github.com/emirpasic/gods/utils"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// consensusID uniquely identifies a public key and role pair to keep track of state.
type consensusID struct {
	SenderID string
	Role     spectypes.RunnerRole
}

// consensusState keeps track of the signers for a given public key and role.
type consensusState struct {
	state    map[spectypes.OperatorID]*treemap.Map // TODO: use *treemap.Map[phase0.Slot, *SignerState] after updating to Go 1.21
	maxSlots phase0.Slot
	mu       sync.Mutex
}

func (cs *consensusState) Get(signer spectypes.OperatorID) *treemap.Map {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if _, ok := cs.state[signer]; !ok {
		cs.state[signer] = treemap.NewWith(slotComparator)
	}

	return cs.state[signer]
}

func slotComparator(a, b interface{}) int {
	return utils.UInt64Comparator(uint64(a.(phase0.Slot)), uint64(b.(phase0.Slot)))
}
