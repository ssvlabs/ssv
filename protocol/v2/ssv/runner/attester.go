package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"

	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
)

type AttesterRunner struct {
	BaseRunner *BaseRunner

	beacon   specssv.BeaconNode
	network  specssv.Network
	signer   spectypes.KeyManager
	valCheck specqbft.ProposedValueCheckF
	logger   *zap.Logger
}

func NewAttesterRunnner(
	beaconNetwork spectypes.BeaconNetwork,
	share *spectypes.Share,
	qbftController *controller.Controller,
	beacon specssv.BeaconNode,
	network specssv.Network,
	signer spectypes.KeyManager,
	valCheck specqbft.ProposedValueCheckF,
) Runner {
	logger := logger.With(zap.String("validator", hex.EncodeToString(share.ValidatorPubKey)))
	return &AttesterRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType: spectypes.BNRoleAttester,
			BeaconNetwork:  beaconNetwork,
			Share:          share,
			QBFTController: qbftController,
			logger:         logger.With(zap.String("who", "BaseRunner")),
		},

		beacon:   beacon,
		network:  network,
		signer:   signer,
		valCheck: valCheck,

		logger: logger.With(zap.String("who", "AttesterRunner")),
	}
}

func (r *AttesterRunner) StartNewDuty(duty *spectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *AttesterRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *AttesterRunner) ProcessPreConsensus(signedMsg *specssv.SignedPartialSignatureMessage) error {
	return errors.New("no pre consensus sigs required for attester role")
}

func (r *AttesterRunner) ProcessConsensus(signedMsg *specqbft.SignedMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	// specific duty sig
	msg, err := r.BaseRunner.signBeaconObject(r, decidedValue.AttestationData, decidedValue.Duty.Slot, spectypes.DomainAttester)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &specssv.PartialSignatureMessages{
		Type:     specssv.PostConsensusPartialSig,
		Messages: []*specssv.PartialSignatureMessage{msg},
	}

	postSignedMsg, err := r.BaseRunner.signPostConsensusMsg(r, postConsensusMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post consensus msg")
	}

	data, err := postSignedMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	msgToBroadcast := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   spectypes.NewMsgID(r.GetShare().ValidatorPubKey, r.BaseRunner.BeaconRoleType),
		Data:    data,
	}

	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *AttesterRunner) ProcessPostConsensus(signedMsg *specssv.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}
	r.logger.Debug("got partial signatures",
		zap.Any("signer", signedMsg.Signer),
		zap.Int64("slot", int64(r.GetState().DecidedValue.Duty.Slot)))

	if !quorum {
		return nil
	}

	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey)
		if err != nil {
			return errors.Wrap(err, "could not reconstruct post consensus signature")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		duty := r.GetState().DecidedValue.Duty

		r.logger.Debug("reconstructed partial signatures",
			zap.Any("signers", getPostConsensusSigners(r.GetState(), root)),
			zap.Int64("slot", int64(duty.Slot)))

		aggregationBitfield := bitfield.NewBitlist(r.GetState().DecidedValue.Duty.CommitteeLength)
		aggregationBitfield.SetBitAt(duty.ValidatorCommitteeIndex, true)
		signedAtt := &phase0.Attestation{
			Data:            r.GetState().DecidedValue.AttestationData,
			Signature:       specSig,
			AggregationBits: aggregationBitfield,
		}

		// Submit it to the BN.
		if err := r.beacon.SubmitAttestation(signedAtt); err != nil {
			r.logger.Error("failed to submit attestation to Beacon node",
				zap.Int64("slot", int64(duty.Slot)), zap.Error(err))
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed attestation")
		}

		r.logger.Debug("successfully submitted attestation",
			zap.Int64("slot", int64(duty.Slot)),
			zap.String("block_root", hex.EncodeToString(signedAtt.Data.BeaconBlockRoot[:])),
			zap.Int("round", int(r.GetState().RunningInstance.State.Round)))
	}
	r.GetState().Finished = true

	return nil
}

func (r *AttesterRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{}, spectypes.DomainError, errors.New("no expected pre consensus roots for attester")
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *AttesterRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{r.BaseRunner.State.DecidedValue.AttestationData}, spectypes.DomainAttester, nil
}

// executeDuty steps:
// 1) get attestation data from BN
// 2) start consensus on duty + attestation data
// 3) Once consensus decides, sign partial attestation and broadcast
// 4) collect 2f+1 partial sigs, reconstruct and broadcast valid attestation sig to the BN
func (r *AttesterRunner) executeDuty(duty *spectypes.Duty) error {
	attData, err := r.GetBeaconNode().GetAttestationData(duty.Slot, duty.CommitteeIndex)
	if err != nil {
		return errors.Wrap(err, "failed to get attestation data")
	}

	input := &spectypes.ConsensusData{
		Duty:            duty,
		AttestationData: attData,
	}

	if err := r.BaseRunner.decide(r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *AttesterRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *AttesterRunner) GetNetwork() specssv.Network {
	return r.network
}

func (r *AttesterRunner) GetBeaconNode() specssv.BeaconNode {
	return r.beacon
}

func (r *AttesterRunner) GetShare() *spectypes.Share {
	return r.BaseRunner.Share
}

func (r *AttesterRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *AttesterRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *AttesterRunner) GetSigner() spectypes.KeyManager {
	return r.signer
}

// Encode returns the encoded struct in bytes or error
func (r *AttesterRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *AttesterRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *AttesterRunner) GetRoot() ([]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}
