package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner/metrics"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type AttesterRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner spectypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF

	metrics metrics.ConsensusMetrics
}

func NewAttesterRunner(
	beaconNetwork spectypes.BeaconNetwork,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specssv.Network,
	signer spectypes.BeaconSigner,
	operatorSigner spectypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) Runner {
	return &AttesterRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     ssvtypes.RoleAttester,
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

		metrics: metrics.NewConsensusMetrics(spectypes.BNRoleAttester),
	}
}

func (r *AttesterRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *AttesterRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *AttesterRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no pre consensus sigs required for attester role")
}

func (r *AttesterRunner) ProcessConsensus(logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	decided, encDecidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}
	r.metrics.EndConsensus()
	r.metrics.StartPostConsensus()

	decidedValue := encDecidedValue.(*spectypes.ConsensusData)

	attestationData, err := r.GetAttestationData(decidedValue)
	if err != nil {
		return errors.Wrap(err, "could not get attestation data")
	}

	msg, err := r.BaseRunner.signBeaconObject(r, r.BaseRunner.State.StartingDuty.(*spectypes.BeaconDuty), attestationData, decidedValue.Duty.Slot, spectypes.DomainAttester)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	msgToBroadcast, err := spectypes.PartialSignatureMessagesToSignedSSVMessage(postConsensusMsg, msgID, r.operatorSigner)
	if err != nil {
		return errors.Wrap(err, "could not sign post-consensus partial signature message")
	}

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}

	return nil
}

func (r *AttesterRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}

	consensusData, err := spectypes.CreateConsensusData(r.GetState().DecidedValue)
	if err != nil {
		return errors.Wrap(err, "could not create consensus data")
	}

	attestationData, err := r.GetAttestationData(consensusData)
	if err != nil {
		return errors.Wrap(err, "could not get attestation data")
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
		r.metrics.EndPostConsensus()

		endSubmission := r.metrics.StartBeaconSubmission()
		startSubmissionTime := time.Now()

		duty := consensusData.Duty
		aggregationBitfield := bitfield.NewBitlist(consensusData.Duty.CommitteeLength)
		aggregationBitfield.SetBitAt(duty.ValidatorCommitteeIndex, true)
		signedAtt := &phase0.Attestation{
			Data:            attestationData,
			Signature:       specSig,
			AggregationBits: aggregationBitfield,
		}

		// Submit it to the BN.
		logger = logger.With(
			zap.Uint64s("signers", getPostConsensusSigners(r.GetState(), root)),
			//fields.BeaconDataTime(r.metrics.GetBeaconDataTime()), // TODO: implement
			//fields.ConsensusTime(r.metrics.GetConsensusTime()),
			//fields.PostConsensusTime(r.metrics.GetPostConsensusTime()),
			fields.Height(r.BaseRunner.QBFTController.Height),
			fields.Round(r.GetState().RunningInstance.State.Round),
			zap.String("block_root", hex.EncodeToString(signedAtt.Data.BeaconBlockRoot[:])),
		)
		if err := r.beacon.SubmitAttestation(signedAtt); err != nil {
			r.metrics.RoleSubmissionFailed()
			logger.Error("❌ failed to submit attestation",
				fields.SubmissionTime(time.Since(startSubmissionTime)),
				zap.Error(err))
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed attestation")
		}

		endSubmission()
		r.metrics.EndDutyFullFlow(r.GetState().RunningInstance.State.Round)
		r.metrics.RoleSubmitted()

		logger.Info("✅ successfully submitted attestation",
			fields.SubmissionTime(time.Since(startSubmissionTime)))
	}
	r.GetState().Finished = true

	return nil
}

func (r *AttesterRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{}, spectypes.DomainError, errors.New("no expected pre consensus roots for attester")
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *AttesterRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	consensusData, err := spectypes.CreateConsensusData(r.GetState().DecidedValue)
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not create consensus data")
	}

	attestationData, err := r.GetAttestationData(consensusData)
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get attestation data")
	}

	return []ssz.HashRoot{attestationData}, spectypes.DomainAttester, nil
}

// executeDuty steps:
// 1) get attestation data from BN
// 2) start consensus on duty + attestation data
// 3) Once consensus decides, sign partial attestation and broadcast
// 4) collect 2f+1 partial sigs, reconstruct and broadcast valid attestation sig to the BN
func (r *AttesterRunner) executeDuty(logger *zap.Logger, duty spectypes.Duty) error {
	//start := time.Now()
	//r.metrics.StartBeaconData() // TODO: implement commented-out code
	beaconDuty := duty.(*spectypes.BeaconDuty)
	attData, ver, err := r.GetBeaconNode().GetAttestationData(beaconDuty.Slot, beaconDuty.CommitteeIndex)
	if err != nil {
		logger.Error("❌ failed to get attestation data",
			//fields.BeaconDataTime(time.Since(start)),
			zap.Error(err))
		return errors.Wrap(err, "failed to get attestation data")
	}
	//r.metrics.EndBeaconData()
	r.metrics.StartDutyFullFlow()
	r.metrics.StartConsensus()

	attDataByts, err := attData.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal attestation data")
	}

	input := &spectypes.ConsensusData{
		Duty:    *beaconDuty,
		Version: ver,
		DataSSZ: attDataByts,
	}

	inputBytes, err := input.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode ConsensusData")
	}

	//logger = logger.With(fields.BeaconDataTime(r.metrics.GetBeaconDataTime()))
	if err := r.BaseRunner.decide(logger, r, input.Duty.Slot, inputBytes); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
	return nil
}

func (r *AttesterRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *AttesterRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *AttesterRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *AttesterRunner) GetShare() *spectypes.Share {
	// TODO better solution for this
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *AttesterRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *AttesterRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *AttesterRunner) GetSigner() spectypes.BeaconSigner {
	return r.signer
}

func (r *AttesterRunner) GetOperatorSigner() spectypes.OperatorSigner {
	return r.operatorSigner
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
func (r *AttesterRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (r *AttesterRunner) GetAttestationData(ci *spectypes.ConsensusData) (*phase0.AttestationData, error) {
	ret := &phase0.AttestationData{}
	if err := ret.UnmarshalSSZ(ci.DataSSZ); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal ssz")
	}
	return ret, nil
}
