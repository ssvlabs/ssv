package runner

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecssv "github.com/bloxapp/ssv-spec-genesis/ssv"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// State holds all the relevant progress the duty execution progress
type State struct {
	PreConsensusContainer  ssvtypes.PartialSigContainer
	PostConsensusContainer ssvtypes.PartialSigContainer
	RunningInstance        *instance.Instance
	DecidedValue           *spectypes.ConsensusData
	// CurrentDuty is the duty the node pulled locally from the beacon node, might be different from decided duty
	StartingDuty spectypes.Duty
	// flags
	Finished bool // Finished marked true when there is a full successful cycle (pre, consensus and post) with quorum
}

func NewRunnerState(quorum uint64, duty spectypes.Duty) *State {
	if true {
		return &State{
			PreConsensusContainer:  &types.GenesisPartialSigContainer{PartialSigContainer: genesisspecssv.NewPartialSigContainer(quorum)},
			PostConsensusContainer: &types.GenesisPartialSigContainer{PartialSigContainer: genesisspecssv.NewPartialSigContainer(quorum)},

			StartingDuty: duty,
			Finished:     false,
		}
	}

	return &State{
		PreConsensusContainer:  &types.AlanPartialSigContainer{PartialSigContainer: specssv.NewPartialSigContainer(quorum)},
		PostConsensusContainer: &types.AlanPartialSigContainer{PartialSigContainer: specssv.NewPartialSigContainer(quorum)},

		StartingDuty: duty,
		Finished:     false,
	}

}

// ReconstructBeaconSig aggregates collected partial beacon sigs
func (pcs *State) ReconstructBeaconSig(container ssvtypes.PartialSigContainer, root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	// Reconstruct signatures
	signature, err := ssvtypes.ReconstructSignature(container, root, validatorPubKey, validatorIndex)
	if err != nil {
		return nil, errors.Wrap(err, "could not reconstruct beacon sig")
	}
	return signature, nil
}

// GetRoot returns the root used for signing and verification
func (pcs *State) GetRoot() ([32]byte, error) {
	marshaledRoot, err := pcs.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode State")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// Encode returns the encoded struct in bytes or error
func (pcs *State) Encode() ([]byte, error) {
	return json.Marshal(pcs)
}

// Decode returns error if decoding failed
func (pcs *State) Decode(data []byte) error {
	return json.Unmarshal(data, &pcs)
}
