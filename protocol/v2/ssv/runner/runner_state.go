package runner

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

// DutyState is a runner state that represents 1 full duty cycle (pre-consensus, consensus and
// post-consensus phases) for some duty.
type DutyState struct {
	PreConsensusContainer  *ssv.PartialSigContainer
	PostConsensusContainer *ssv.PartialSigContainer
	// QBFTInstance is initialized once runner enters QBFT consensus phase for the duty this
	// DutyState represents.
	QBFTInstance *instance.Instance
	// CurrentDuty is the duty the node pulled locally from the beacon node, might be different from decided duty
	StartingDuty spectypes.Duty `json:"StartingDuty,omitempty"`
	// Finished is true when runner has successfully finished full duty cycle (pre-consensus,
	// consensus and post-consensus phases) but has not started new duty yet, and false otherwise.
	Finished bool
}

func NewDutyState(quorum uint64, duty spectypes.Duty) *DutyState {
	return &DutyState{
		PreConsensusContainer:  ssv.NewPartialSigContainer(quorum),
		PostConsensusContainer: ssv.NewPartialSigContainer(quorum),
		QBFTInstance:           nil, // will get initialized once runner enters QBFT consensus phase
		StartingDuty:           duty,
		Finished:               false,
	}
}

// DecidedValue returns the value decided upon by QBFT consensus phase of DutyState.QBFTInstance
// (if any). It is equivalent to the value DutyState.QBFTInstance.DutyState.DecidedValue has - and we
// simply have a helper-method to extract it.
// Runner(s) encode/decode DecidedValue with spectypes.Encoder.
func (pcs *DutyState) DecidedValue() (decided bool, value []byte) {
	if pcs.QBFTInstance == nil {
		return false, nil
	}
	return pcs.QBFTInstance.IsDecided()
}

// ReconstructBeaconSig aggregates collected partial beacon sigs
func (pcs *DutyState) ReconstructBeaconSig(container *ssv.PartialSigContainer, root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	// Reconstruct signatures
	signature, err := container.ReconstructSignature(root, validatorPubKey, validatorIndex)
	if err != nil {
		return nil, errors.Wrap(err, "could not reconstruct beacon sig")
	}
	return signature, nil
}

// GetRoot returns the root used for signing and verification
func (pcs *DutyState) GetRoot() ([32]byte, error) {
	marshaledRoot, err := pcs.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// Encode returns the encoded struct in bytes or error
func (pcs *DutyState) Encode() ([]byte, error) {
	return json.Marshal(pcs)
}

// Decode returns error if decoding failed
func (pcs *DutyState) Decode(data []byte) error {
	return json.Unmarshal(data, &pcs)
}
