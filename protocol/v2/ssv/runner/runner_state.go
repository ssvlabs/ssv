package runner

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

// State holds all the relevant progress the duty execution progress
type State struct {
	PreConsensusContainer  *ssv.PartialSigContainer
	PostConsensusContainer *ssv.PartialSigContainer
	RunningInstance        *instance.Instance
	DecidedValue           []byte //spectypes.Encoder
	// CurrentDuty is the duty the node pulled locally from the beacon node, might be different
	// from the actual duty operators will have decided upon.
	CurrentDuty spectypes.Duty `json:"CurrentDuty,omitempty"`
	// Succeeded is true when the runner has executed the duty to a 100% completion successfully.
	// It is serialized as "Finished" (see MarshalJSON/UnmarshalJSON) to stay byte-compatible with
	// ssv-spec's State.Finished, which the spec tests compare via the post-duty-runner-state root.
	Succeeded bool
}

func NewRunnerState(quorum uint64, duty spectypes.Duty) *State {
	return &State{
		PreConsensusContainer:  ssv.NewPartialSigContainer(quorum),
		PostConsensusContainer: ssv.NewPartialSigContainer(quorum),
		CurrentDuty:            duty,
		Succeeded:              false,
	}
}

// ReconstructBeaconSig aggregates collected partial beacon sigs
func (pcs *State) ReconstructBeaconSig(container *ssv.PartialSigContainer, root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	signature, err := container.ReconstructSignature(root, validatorPubKey, validatorIndex)
	if err != nil {
		return nil, fmt.Errorf("could not reconstruct beacon sig: %w", err)
	}
	return signature, nil
}

// GetRoot returns the root used for signing and verification
func (pcs *State) GetRoot() ([32]byte, error) {
	marshaledRoot, err := pcs.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode State: %w", err)
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

func (pcs *State) MarshalJSON() ([]byte, error) {
	// Create alias without duty
	type StateAlias struct {
		PreConsensusContainer   *ssv.PartialSigContainer
		PostConsensusContainer  *ssv.PartialSigContainer
		RunningInstance         *instance.Instance
		DecidedValue            []byte
		Finished                bool
		ValidatorDuty           *spectypes.ValidatorDuty           `json:"ValidatorDuty,omitempty"`
		CommitteeDuty           *spectypes.CommitteeDuty           `json:"CommitteeDuty,omitempty"`
		AggregatorCommitteeDuty *spectypes.AggregatorCommitteeDuty `json:"AggregatorCommitteeDuty,omitempty"`
	}

	alias := &StateAlias{
		PreConsensusContainer:  pcs.PreConsensusContainer,
		PostConsensusContainer: pcs.PostConsensusContainer,
		RunningInstance:        pcs.RunningInstance,
		DecidedValue:           pcs.DecidedValue,
		Finished:               pcs.Succeeded,
	}

	if pcs.CurrentDuty != nil {
		if ValidatorDuty, ok := pcs.CurrentDuty.(*spectypes.ValidatorDuty); ok {
			alias.ValidatorDuty = ValidatorDuty
		} else if committeeDuty, ok := pcs.CurrentDuty.(*spectypes.CommitteeDuty); ok {
			alias.CommitteeDuty = committeeDuty
		} else if aggCommDuty, ok := pcs.CurrentDuty.(*spectypes.AggregatorCommitteeDuty); ok {
			alias.AggregatorCommitteeDuty = aggCommDuty
		} else {
			return nil, errors.New("can't marshal because BaseRunner.State.CurrentDuty isn't a supported duty type")
		}
	}

	return json.Marshal(alias)
}

func (pcs *State) UnmarshalJSON(data []byte) error {
	// Create alias without duty
	type StateAlias struct {
		PreConsensusContainer   *ssv.PartialSigContainer
		PostConsensusContainer  *ssv.PartialSigContainer
		RunningInstance         *instance.Instance
		DecidedValue            []byte
		Finished                bool
		ValidatorDuty           *spectypes.ValidatorDuty           `json:"ValidatorDuty,omitempty"`
		CommitteeDuty           *spectypes.CommitteeDuty           `json:"CommitteeDuty,omitempty"`
		AggregatorCommitteeDuty *spectypes.AggregatorCommitteeDuty `json:"AggregatorCommitteeDuty,omitempty"`
	}

	aux := &StateAlias{}

	// Unmarshal the JSON data into the auxiliary struct
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	pcs.PreConsensusContainer = aux.PreConsensusContainer
	pcs.PostConsensusContainer = aux.PostConsensusContainer
	pcs.RunningInstance = aux.RunningInstance
	pcs.DecidedValue = aux.DecidedValue
	pcs.Succeeded = aux.Finished

	// Determine which type of duty was marshaled
	if aux.ValidatorDuty != nil {
		pcs.CurrentDuty = aux.ValidatorDuty
	} else if aux.CommitteeDuty != nil {
		pcs.CurrentDuty = aux.CommitteeDuty
	} else if aux.AggregatorCommitteeDuty != nil {
		pcs.CurrentDuty = aux.AggregatorCommitteeDuty
	} else {
		return fmt.Errorf("no starting duty in state JSON")
	}

	return nil
}
