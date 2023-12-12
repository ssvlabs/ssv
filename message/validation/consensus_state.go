package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
)

// ConsensusID uniquely identifies a public key and role pair to keep track of state.
type ConsensusID struct {
	PubKey phase0.BLSPubKey
	Role   spectypes.BeaconRole
}

// ConsensusState keeps track of the signers for a given public key and role.
type ConsensusState struct {
	// TODO: consider evicting old data to avoid excessive memory consumption
	Signers *hashmap.Map[spectypes.OperatorID, *SignerState]
}

// GetSignerState retrieves the state for the given signer.
// Returns nil if the signer is not found.
func (cs *ConsensusState) GetSignerState(signer spectypes.OperatorID) *SignerState {
	signerState, _ := cs.Signers.Get(signer)
	return signerState
}

// CreateSignerState initializes and sets a new SignerState for the given signer.
func (cs *ConsensusState) CreateSignerState(signer spectypes.OperatorID) *SignerState {
	signerState := &SignerState{}
	cs.Signers.Set(signer, signerState)

	return signerState
}
