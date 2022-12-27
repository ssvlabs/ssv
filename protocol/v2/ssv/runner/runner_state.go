package runner

import (
	"crypto/sha256"
	"encoding/json"
	"sync/atomic"
	"unsafe"

	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	uberatomic "go.uber.org/atomic" // TODO: remove after migration to Go 1.19

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

// State holds all the relevant progress the duty execution progress
type State struct {
	PreConsensusContainer  *specssv.PartialSigContainer
	PostConsensusContainer *specssv.PartialSigContainer
	RunningInstance        *instance.Instance // TODO: use atomic.Pointer from sync/atomic after migration to Go 1.19
	DecidedValue           *spectypes.ConsensusData
	// CurrentDuty is the duty the node pulled locally from the beacon node, might be different from decided duty
	StartingDuty *spectypes.Duty
	// flags
	Finished uberatomic.Bool // Finished marked true when there is a full successful cycle (pre, consensus and post) with quorum, TODO: use Bool from sync/atomic after migration to Go 1.19
}

func NewRunnerState(quorum uint64, duty *spectypes.Duty) *State {
	return &State{
		PreConsensusContainer:  specssv.NewPartialSigContainer(quorum),
		PostConsensusContainer: specssv.NewPartialSigContainer(quorum),

		StartingDuty: duty,
	}
}

// GetRunningInstance gets RunningInstance atomically.
// TODO: remove after migration to Go 1.19
func (pcs *State) GetRunningInstance() *instance.Instance {
	return (*instance.Instance)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&pcs.RunningInstance))))
}

// SetRunningInstance sets RunningInstance atomically.
// TODO: remove after migration to Go 1.19
func (pcs *State) SetRunningInstance(v *instance.Instance) {
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&pcs.RunningInstance)), unsafe.Pointer(v))
}

// ReconstructBeaconSig aggregates collected partial beacon sigs
func (pcs *State) ReconstructBeaconSig(container *specssv.PartialSigContainer, root, validatorPubKey []byte) ([]byte, error) {
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
