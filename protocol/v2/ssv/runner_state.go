package ssv

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
)

// State holds all the relevant progress the duty execution progress
type State struct {
	PreConsensusContainer  *PartialSigContainer
	PostConsensusContainer *PartialSigContainer
	RunningInstance        *qbft.Instance
	DecidedValue           *types.ConsensusData
	// CurrentDuty is the duty the node pulled locally from the beacon node, might be different from decided duty
	StartingDuty *types.Duty
	// flags
	Finished bool // Finished marked true when there is a full successful cycle (pre, consensus and post) with quorum
}

func NewRunnerState(quorum uint64, duty *types.Duty) *State {
	return &State{
		PreConsensusContainer:  NewPartialSigContainer(quorum),
		PostConsensusContainer: NewPartialSigContainer(quorum),

		StartingDuty: duty,
		Finished:     false,
	}
}

// ReconstructBeaconSig aggregates collected partial beacon sigs
func (pcs *State) ReconstructBeaconSig(container *PartialSigContainer, root, validatorPubKey []byte) ([]byte, error) {
	// Reconstruct signatures
	signature, err := container.ReconstructSignature(root, validatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not reconstruct beacon sig")
	}
	return signature, nil
}

// GetRoot returns the root used for signing and verification
func (pcs *State) GetRoot() ([]byte, error) {
	marshaledRoot, err := pcs.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode State")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

// Encode returns the encoded struct in bytes or error
func (pcs *State) Encode() ([]byte, error) {
	return json.Marshal(pcs)
}

// Decode returns error if decoding failed
func (pcs *State) Decode(data []byte) error {
	return json.Unmarshal(data, &pcs)
}
