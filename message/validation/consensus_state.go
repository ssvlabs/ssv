package validation

import (
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// consensusID uniquely identifies a public key and role pair to keep track of state.
type consensusID struct {
	DutyExecutorID string
	Role           spectypes.RunnerRole
}

// consensusState keeps track of the signers for a given public key and role.
type consensusState struct {
	state           map[spectypes.OperatorID][]*SignerState // the slice index is slot % storedSlotCount
	storedSlotCount phase0.Slot
	mu              sync.Mutex
}

func (cs *consensusState) GetOrCreate(signer spectypes.OperatorID) []*SignerState {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if _, ok := cs.state[signer]; !ok {
		cs.state[signer] = make([]*SignerState, cs.storedSlotCount)
	}

	return cs.state[signer]
}
