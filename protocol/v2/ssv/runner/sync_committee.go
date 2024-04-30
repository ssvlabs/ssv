package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	specssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner/metrics"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type SyncCommitteeRunner struct {
	BaseRunner *BaseRunner

	beacon         specssv.BeaconNode
	network        specssv.Network
	signer         genesisspectypes.KeyManager
	beaconSigner   spectypes.BeaconSigner
	operatorSigner spectypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF

	metrics metrics.ConsensusMetrics
}

func NewSyncCommitteeRunner(
	beaconNetwork spectypes.BeaconNetwork,
	shares *map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon specssv.BeaconNode,
	network specssv.Network,
	signer genesisspectypes.KeyManager,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) Runner {
	return &SyncCommitteeRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType:     genesisspectypes.BNRoleSyncCommittee,
			BeaconNetwork:      beaconNetwork,
			Shares:             *shares,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},

		beacon:   beacon,
		network:  network,
		signer:   signer,
		valCheck: valCheck,
		metrics:  metrics.NewConsensusMetrics(spectypes.BNRoleSyncCommittee),
	}
}

func (r *SyncCommitteeRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *SyncCommitteeRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *SyncCommitteeRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg ssvtypes.PartialSignatureMessages) error {
	return errors.New("no pre consensus sigs required for sync committee role")
}

func (r *SyncCommitteeRunner) ProcessConsensus(logger *zap.Logger, signedMsg ssvtypes.SignedMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.metrics.EndConsensus()
	r.metrics.StartPostConsensus()

	// specific duty sig
	root, err := GetSyncCommitteeBlockRoot(decidedValue.(*spectypes.ConsensusData))
	if err != nil {
		return errors.Wrap(err, "could not get sync committee block root")
	}
	msg, err := r.BaseRunner.signBeaconObject(r, r.BaseRunner.State.StartingDuty.(*spectypes.BeaconDuty),
		spectypes.SSZBytes(root[:]),
		decidedValue.(*spectypes.ConsensusData).Duty.Slot,
		spectypes.DomainSyncCommittee)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     decidedValue.(*spectypes.ConsensusData).Duty.DutySlot(),
		Messages: []*genesisspectypes.PartialSignatureMessage{msg.(*genesisspectypes.PartialSignatureMessage)},
	}

	postSignedMsg, err := r.BaseRunner.signPostConsensusMsg(r, postConsensusMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post consensus msg")
	}

	data, err := postSignedMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	msgToBroadcast := &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.SSVPartialSignatureMsgType,
		MsgID:   genesisspectypes.NewMsgID(genesisspectypes.DomainType(r.GetShare().DomainType), r.GetShare().ValidatorPubKey[:], r.BaseRunner.BeaconRoleType),
		Data:    data,
	}

	if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *SyncCommitteeRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg ssvtypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}

	r.metrics.EndPostConsensus()

	consensusData, err := spectypes.CreateConsensusData(r.GetState().DecidedValue)
	if err != nil {
		return errors.Wrap(err, "could not create consensus data")
	}
	blockRoot, err := GetSyncCommitteeBlockRoot(consensusData)
	if err != nil {
		return errors.Wrap(err, "could not get sync committee block root")
	}

	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
			}
			return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		msg := &altair.SyncCommitteeMessage{
			Slot:            consensusData.Duty.DutySlot(),
			BeaconBlockRoot: blockRoot,
			ValidatorIndex:  consensusData.Duty.ValidatorIndex,
			Signature:       specSig,
		}

		messageSubmissionEnd := r.metrics.StartBeaconSubmission()

		if err := r.GetBeaconNode().SubmitSyncMessage(msg); err != nil {
			r.metrics.RoleSubmissionFailed()
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed sync committee")
		}

		messageSubmissionEnd()
		r.metrics.EndDutyFullFlow(r.GetState().RunningInstance.State.Round)
		r.metrics.RoleSubmitted()

		logger.Info("✅ successfully submitted sync committee",
			fields.Slot(msg.Slot),
			zap.String("block_root", hex.EncodeToString(msg.BeaconBlockRoot[:])),
			fields.Height(r.BaseRunner.QBFTController.Height),
			fields.Round(r.GetState().RunningInstance.State.Round))
	}
	r.GetState().Finished = true

	return nil
}

func (r *SyncCommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{}, spectypes.DomainError, errors.New("no expected pre consensus roots for sync committee")
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *SyncCommitteeRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	consensusData, err := spectypes.CreateConsensusData(r.GetState().DecidedValue)
	if err != nil {
		return nil, spectypes.DomainError, errors.Wrap(err, "could not create consensus data")
	}
	root, err := GetSyncCommitteeBlockRoot(consensusData)
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get sync committee block root")
	}

	return []ssz.HashRoot{spectypes.SSZBytes(root[:])}, spectypes.DomainSyncCommittee, nil
}

// executeDuty steps:
// 1) get sync block root from BN
// 2) start consensus on duty + block root data
// 3) Once consensus decides, sign partial block root and broadcast
// 4) collect 2f+1 partial sigs, reconstruct and broadcast valid sync committee sig to the BN
func (r *SyncCommitteeRunner) executeDuty(logger *zap.Logger, duty spectypes.Duty) error {
	// TODO - waitOneThirdOrValidBlock

	root, ver, err := r.GetBeaconNode().GetSyncMessageBlockRoot(duty.DutySlot())
	if err != nil {
		return errors.Wrap(err, "failed to get sync committee block root")
	}

	r.metrics.StartDutyFullFlow()
	r.metrics.StartConsensus()

	input := &spectypes.ConsensusData{
		Duty:    *duty.(*spectypes.BeaconDuty),
		Version: ver,
		DataSSZ: root[:],
	}

	if err := r.BaseRunner.decide(logger, r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
	return nil
}

func (r *SyncCommitteeRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *SyncCommitteeRunner) GetNetwork() specssv.Network {
	return r.network
}

func (r *SyncCommitteeRunner) GetBeaconNode() specssv.BeaconNode {
	return r.beacon
}

func (r *SyncCommitteeRunner) GetShare() *spectypes.Share {
	for _, share := range r.BaseRunner.Shares {
		return share
	}
	return nil
}

func (r *SyncCommitteeRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *SyncCommitteeRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *SyncCommitteeRunner) GetGenesisSigner() genesisspectypes.KeyManager {
	return r.signer
}

func (r *SyncCommitteeRunner) GetSigner() spectypes.BeaconSigner {
	return r.beaconSigner
}

func (r *SyncCommitteeRunner) GetOperatorSigner() spectypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *SyncCommitteeRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *SyncCommitteeRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *SyncCommitteeRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func GetSyncCommitteeBlockRoot(ci *spectypes.ConsensusData) (phase0.Root, error) {
	ret := spectypes.SSZ32Bytes{}
	if err := ret.UnmarshalSSZ(ci.DataSSZ); err != nil {
		return phase0.Root{}, errors.Wrap(err, "could not unmarshal ssz")
	}
	return phase0.Root(ret), nil
}
