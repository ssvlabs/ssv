package validation

import (
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/alan/types"
)

// consensusID uniquely identifies a public key and role pair to keep track of state.
type consensusID struct {
	SenderID string
	Role     spectypes.RunnerRole
}

// consensusState keeps track of the signers for a given public key and role.
type consensusState struct {
	// TODO: consider evicting old data to avoid excessive memory consumption
	signers map[spectypes.OperatorID]*SignerState
	mu      sync.Mutex
}

// GetSignerState retrieves the state for the given signer.
// Returns nil if the signer is not found.
func (cs *consensusState) GetSignerState(signer spectypes.OperatorID) *SignerState {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	return cs.signers[signer]
}

// CreateSignerState initializes and sets a new SignerState for the given signer.
func (cs *consensusState) CreateSignerState(signer spectypes.OperatorID) *SignerState {
	signerState := &SignerState{}

	cs.mu.Lock()
	cs.signers[signer] = signerState
	cs.mu.Unlock()

	return signerState
}
