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
	signers map[spectypes.OperatorID]SignerState
	mu      sync.Mutex
}

// GetSignerState retrieves the state for the given signer.
// Returns nil if the signer is not found.
func (cs *consensusState) GetSignerState(signer spectypes.OperatorID) SignerState {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if _, ok := cs.signers[signer]; !ok {
		cs.signers[signer] = SignerState{}
	}

	return cs.signers[signer]
}
