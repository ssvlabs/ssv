package genesisrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspecssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/runner/metrics"
)

type SyncCommitteeRunner struct {
	BaseRunner *BaseRunner

	beacon         genesisspecssv.BeaconNode
	network        genesisspecssv.Network
	signer         genesisspectypes.KeyManager
	operatorSigner OperatorSigner
	valCheck       genesisspecqbft.ProposedValueCheckF

	metrics metrics.ConsensusMetrics
}

func NewSyncCommitteeRunner(
	beaconNetwork genesisspectypes.BeaconNetwork,
	share *spectypes.Share,
	qbftController *controller.Controller,
	beacon genesisspecssv.BeaconNode,
	network genesisspecssv.Network,
	signer genesisspectypes.KeyManager,
	operatorSigner OperatorSigner,
	valCheck genesisspecqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) Runner {
	return &SyncCommitteeRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType:     genesisspectypes.BNRoleSyncCommittee,
			BeaconNetwork:      beaconNetwork,
			Share:              share,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},

		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
		valCheck:       valCheck,

		metrics: metrics.NewConsensusMetrics(genesisspectypes.BNRoleSyncCommittee),
	}
}

func (r *SyncCommitteeRunner) StartNewDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *SyncCommitteeRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *SyncCommitteeRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	return errors.New("no pre consensus sigs required for sync committee role")
}

func (r *SyncCommitteeRunner) ProcessConsensus(logger *zap.Logger, signedMsg *genesisspecqbft.SignedMessage) error {
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
	root, err := decidedValue.GetSyncCommitteeBlockRoot()
	if err != nil {
		return errors.Wrap(err, "could not get sync committee block root")
	}
	msg, err := r.BaseRunner.signBeaconObject(r, genesisspectypes.SSZBytes(root[:]), decidedValue.Duty.Slot, genesisspectypes.DomainSyncCommittee)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*genesisspectypes.PartialSignatureMessage{msg},
	}

	postSignedMsg, err := r.BaseRunner.signPostConsensusMsg(r, postConsensusMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post consensus msg")
	}

	data, err := postSignedMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	ssvMsg := &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.SSVPartialSignatureMsgType,
		MsgID:   genesisspectypes.NewMsgID(genesisspectypes.DomainType(r.GetShare().DomainType), r.GetShare().ValidatorPubKey[:], r.BaseRunner.BeaconRoleType),
		Data:    data,
	}

	msgToBroadcast, err := genesisspectypes.SSVMessageToSignedSSVMessage(ssvMsg, r.operatorSigner.GetOperatorID(), r.operatorSigner.SignSSVMessage)
	if err != nil {
		return errors.Wrap(err, "could not create SignedSSVMessage from SSVMessage")
	}

	if err := r.GetNetwork().Broadcast(ssvMsg.GetID(), msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (r *SyncCommitteeRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}

	blockRoot, err := r.GetState().DecidedValue.GetSyncCommitteeBlockRoot()
	if err != nil {
		return errors.Wrap(err, "could not get sync committee block root")
	}

	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:])
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root)
			}
			return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)
		r.metrics.EndPostConsensus()

		endSubmission := r.metrics.StartBeaconSubmission()
		start := time.Now()
		msg := &altair.SyncCommitteeMessage{
			Slot:            r.GetState().DecidedValue.Duty.Slot,
			BeaconBlockRoot: blockRoot,
			ValidatorIndex:  r.GetState().DecidedValue.Duty.ValidatorIndex,
			Signature:       specSig,
		}

		logger = logger.With(
			zap.Uint64s("signers", getPostConsensusSigners(r.GetState(), root)),
			fields.BeaconDataTime(r.metrics.GetBeaconDataTime()),
			fields.ConsensusTime(r.metrics.GetConsensusTime()),
			fields.PostConsensusTime(r.metrics.GetPostConsensusTime()),
			fields.Height(specqbft.Height(r.BaseRunner.QBFTController.Height)),
			fields.Round(specqbft.Round(r.GetState().RunningInstance.State.Round)),
			zap.String("block_root", hex.EncodeToString(msg.BeaconBlockRoot[:])),
		)
		if err := r.GetBeaconNode().SubmitSyncMessage(msg); err != nil {
			r.metrics.RoleSubmissionFailed()
			logger.Error("❌ failed to submit sync message",
				fields.SubmissionTime(time.Since(start)),
				zap.Error(err))
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed sync committee")
		}

		endSubmission()
		r.metrics.EndDutyFullFlow(specqbft.Round(r.GetState().RunningInstance.State.Round))
		r.metrics.RoleSubmitted()

		logger.Info("✅ successfully submitted sync committee",
			fields.SubmissionTime(time.Since(start)))
	}
	r.GetState().Finished = true

	return nil
}

func (r *SyncCommitteeRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{}, genesisspectypes.DomainError, errors.New("no expected pre consensus roots for sync committee")
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *SyncCommitteeRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	root, err := r.GetState().DecidedValue.GetSyncCommitteeBlockRoot()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get sync committee block root")
	}

	return []ssz.HashRoot{genesisspectypes.SSZBytes(root[:])}, genesisspectypes.DomainSyncCommittee, nil
}

// executeDuty steps:
// 1) get sync block root from BN
// 2) start consensus on duty + block root data
// 3) Once consensus decides, sign partial block root and broadcast
// 4) collect 2f+1 partial sigs, reconstruct and broadcast valid sync committee sig to the BN
func (r *SyncCommitteeRunner) executeDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	// TODO - waitOneThirdOrValidBlock

	r.metrics.StartBeaconData()
	root, ver, err := r.GetBeaconNode().GetSyncMessageBlockRoot(duty.Slot)
	if err != nil {
		return errors.Wrap(err, "failed to get sync committee block root")
	}
	r.metrics.EndBeaconData()

	r.metrics.StartDutyFullFlow()
	r.metrics.StartConsensus()

	input := &genesisspectypes.ConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: root[:],
	}

	logger = logger.With(fields.BeaconDataTime(r.metrics.GetBeaconDataTime()))
	if err := r.BaseRunner.decide(logger, r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
	return nil
}

func (r *SyncCommitteeRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *SyncCommitteeRunner) GetNetwork() genesisspecssv.Network {
	return r.network
}

func (r *SyncCommitteeRunner) GetBeaconNode() genesisspecssv.BeaconNode {
	return r.beacon
}

func (r *SyncCommitteeRunner) GetShare() *spectypes.Share {
	return r.BaseRunner.Share
}

func (r *SyncCommitteeRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *SyncCommitteeRunner) GetValCheckF() genesisspecqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *SyncCommitteeRunner) GetSigner() genesisspectypes.KeyManager {
	return r.signer
}

func (r *SyncCommitteeRunner) GetOperatorSigner() OperatorSigner {
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
