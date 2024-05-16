package validation

import (
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// consensusID uniquely identifies a public key and role pair to keep track of state.
type consensusID struct {
	SenderID string
	Role     spectypes.RunnerRole
}

// consensusState keeps track of the signers for a given public key and role.
type consensusState struct {
	state map[spectypes.OperatorID]*SignerState
	mu    sync.Mutex
}

// GetSignerState retrieves the state for the given slot and signer.
// Creates state if not found.
func (cs *consensusState) GetSignerState(signer spectypes.OperatorID) *SignerState {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if _, ok := cs.state[signer]; !ok {
		cs.state[signer] = &SignerState{}
	}

	return cs.state[signer]
}
