package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspecssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/runner/metrics"
)

// Duty runner for validator voluntary exit duty
type VoluntaryExitRunner struct {
	BaseRunner *BaseRunner

	beacon   genesisspecssv.BeaconNode
	network  genesisspecssv.Network
	signer   genesisspectypes.KeyManager
	valCheck genesisspecqbft.ProposedValueCheckF

	voluntaryExit *phase0.VoluntaryExit

	metrics metrics.ConsensusMetrics
}

func NewVoluntaryExitRunner(
	beaconNetwork genesisspectypes.BeaconNetwork,
	share *genesisspectypes.Share,
	beacon genesisspecssv.BeaconNode,
	network genesisspecssv.Network,
	signer genesisspectypes.KeyManager,
) Runner {
	return &VoluntaryExitRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType: genesisspectypes.BNRoleVoluntaryExit,
			BeaconNetwork:  beaconNetwork,
			Share:          share,
		},

		beacon:  beacon,
		network: network,
		signer:  signer,
		metrics: metrics.NewConsensusMetrics(genesisspectypes.BNRoleVoluntaryExit),
	}
}

func (r *VoluntaryExitRunner) StartNewDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	return r.BaseRunner.baseStartNewNonBeaconDuty(logger, r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *VoluntaryExitRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

// Check for quorum of partial signatures over VoluntaryExit and,
// if has quorum, constructs SignedVoluntaryExit and submits to BeaconNode
func (r *VoluntaryExitRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing voluntary exit message")
	}

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], fullSig)

	// create SignedVoluntaryExit using VoluntaryExit created on r.executeDuty() and reconstructed signature
	signedVoluntaryExit := &phase0.SignedVoluntaryExit{
		Message:   r.voluntaryExit,
		Signature: specSig,
	}

	if err := r.beacon.SubmitVoluntaryExit(signedVoluntaryExit); err != nil {
		return errors.Wrap(err, "could not submit voluntary exit")
	}

	logger.Debug("voluntary exit submitted successfully",
		fields.Epoch(r.voluntaryExit.Epoch),
		zap.Uint64("validator_index", uint64(r.voluntaryExit.ValidatorIndex)),
		zap.String("signature", hex.EncodeToString(specSig[:])),
	)

	r.GetState().Finished = true
	return nil
}

func (r *VoluntaryExitRunner) ProcessConsensus(logger *zap.Logger, signedMsg *genesisspecqbft.SignedMessage) error {
	return errors.New("no consensus phase for voluntary exit")
}

func (r *VoluntaryExitRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	return errors.New("no post consensus phase for voluntary exit")
}

func (r *VoluntaryExitRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	vr, err := r.calculateVoluntaryExit()
	if err != nil {
		return nil, genesisspectypes.DomainError, errors.Wrap(err, "could not calculate voluntary exit")
	}
	return []ssz.HashRoot{vr}, genesisspectypes.DomainVoluntaryExit, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *VoluntaryExitRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, [4]byte{}, errors.New("no post consensus roots for voluntary exit")
}

// Validator voluntary exit duty doesn't need consensus nor post-consensus.
// It just performs pre-consensus with VoluntaryExitPartialSig over
// a VoluntaryExit object to create a SignedVoluntaryExit
func (r *VoluntaryExitRunner) executeDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	voluntaryExit, err := r.calculateVoluntaryExit()
	if err != nil {
		return errors.Wrap(err, "could not calculate voluntary exit")
	}

	// get PartialSignatureMessage with voluntaryExit root and signature
	msg, err := r.BaseRunner.signBeaconObject(r, voluntaryExit, duty.Slot, genesisspectypes.DomainVoluntaryExit)
	if err != nil {
		return errors.Wrap(err, "could not sign VoluntaryExit object")
	}

	msgs := genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.VoluntaryExitPartialSig,
		Slot:     duty.Slot,
		Messages: []*genesisspectypes.PartialSignatureMessage{msg},
	}

	// sign PartialSignatureMessages object
	signature, err := r.GetSigner().SignRoot(msgs, genesisspectypes.PartialSignatureType, r.GetShare().SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign randao msg")
	}
	signedPartialMsg := &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: signature,
		Signer:    r.GetShare().OperatorID,
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode signedPartialMsg with VoluntaryExit")
	}
	msgToBroadcast := &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.SSVPartialSignatureMsgType,
		MsgID:   genesisspectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}
	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast signedPartialMsg with VoluntaryExit")
	}

	// stores value for later using in ProcessPreConsensus
	r.voluntaryExit = voluntaryExit

	return nil
}

// Returns *phase0.VoluntaryExit object with current epoch and own validator index
func (r *VoluntaryExitRunner) calculateVoluntaryExit() (*phase0.VoluntaryExit, error) {
	epoch := r.BaseRunner.BeaconNetwork.EstimatedEpochAtSlot(r.BaseRunner.State.StartingDuty.Slot)
	validatorIndex := r.GetState().StartingDuty.ValidatorIndex
	return &phase0.VoluntaryExit{
		Epoch:          epoch,
		ValidatorIndex: validatorIndex,
	}, nil
}

func (r *VoluntaryExitRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *VoluntaryExitRunner) GetNetwork() genesisspecssv.Network {
	return r.network
}

func (r *VoluntaryExitRunner) GetBeaconNode() genesisspecssv.BeaconNode {
	return r.beacon
}

func (r *VoluntaryExitRunner) GetShare() *genesisspectypes.Share {
	return r.BaseRunner.Share
}

func (r *VoluntaryExitRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *VoluntaryExitRunner) GetValCheckF() genesisspecqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *VoluntaryExitRunner) GetSigner() genesisspectypes.KeyManager {
	return r.signer
}

// Encode returns the encoded struct in bytes or error
func (r *VoluntaryExitRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *VoluntaryExitRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *VoluntaryExitRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}
