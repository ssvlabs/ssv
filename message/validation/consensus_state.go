package validation

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/jellydator/ttlcache/v3"
)

// ConsensusID uniquely identifies a public key and role pair to keep track of state.
type ConsensusID struct {
	PubKey phase0.BLSPubKey
	Role   spectypes.BeaconRole
}

// ConsensusState keeps track of the signers for a given public key and role.
type ConsensusState struct {
	Signers  *ttlcache.Cache[spectypes.OperatorID, *SignerState]
	cacheTTL time.Duration
}

// GetSignerState retrieves the state for the given signer.
// Returns nil if the signer is not found.
func (cs *ConsensusState) GetSignerState(signer spectypes.OperatorID) (*SignerState, bool) {
	signerState := cs.Signers.Get(signer)
	if signerState == nil {
		return nil, false
	}
	return signerState.Value(), true
}

// CreateSignerState initializes and sets a new SignerState for the given signer.
func (cs *ConsensusState) CreateSignerState(signer spectypes.OperatorID) *SignerState {
	signerState := &SignerState{}
	cs.Signers.Set(signer, signerState, cs.cacheTTL)

	return signerState
}
