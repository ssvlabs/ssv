package genesisrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

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
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

type AggregatorRunner struct {
	BaseRunner *BaseRunner

	beacon         genesisspecssv.BeaconNode
	network        genesisspecssv.Network
	signer         genesisspectypes.KeyManager
	operatorSigner types.OperatorSigner
	valCheck       genesisspecqbft.ProposedValueCheckF

	metrics metrics.ConsensusMetrics
}

var _ Runner = &AggregatorRunner{}

func NewAggregatorRunner(
	beaconNetwork genesisspectypes.BeaconNetwork,
	share *spectypes.Share,
	qbftController *controller.Controller,
	beacon genesisspecssv.BeaconNode,
	network genesisspecssv.Network,
	signer genesisspectypes.KeyManager,
	operatorSigner types.OperatorSigner,
	valCheck genesisspecqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) Runner {
	return &AggregatorRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType:     genesisspectypes.BNRoleAggregator,
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

		metrics: metrics.NewConsensusMetrics(genesisspectypes.BNRoleAggregator),
	}
}

func (r *AggregatorRunner) StartNewDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *AggregatorRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *AggregatorRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing selection proof message")
	}
	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	// only 1 root, verified by basePreConsensusMsgProcessing
	root := roots[0]
	// reconstruct selection proof sig
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:])
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}
	r.metrics.EndPreConsensus()

	r.metrics.PauseDutyFullFlow()
	// get block data
	duty := r.GetState().StartingDuty
	res, ver, err := r.GetBeaconNode().SubmitAggregateSelectionProof(duty.Slot, duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return errors.Wrap(err, "failed to submit aggregate and proof")
	}
	r.metrics.ContinueDutyFullFlow()

	byts, err := res.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal aggregate and proof")
	}
	input := &genesisspectypes.ConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	r.metrics.StartConsensus()
	if err := r.BaseRunner.decide(logger, r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *AggregatorRunner) ProcessConsensus(logger *zap.Logger, signedMsg *genesisspecqbft.SignedMessage) error {
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

	aggregateAndProof, err := decidedValue.GetAggregateAndProof()
	if err != nil {
		return errors.Wrap(err, "could not get aggregate and proof")
	}

	// specific duty sig
	msg, err := r.BaseRunner.signBeaconObject(r, aggregateAndProof, decidedValue.Duty.Slot, genesisspectypes.DomainAggregateAndProof)
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

func (r *AggregatorRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}

	r.metrics.EndPostConsensus()

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

		aggregateAndProof, err := r.GetState().DecidedValue.GetAggregateAndProof()
		if err != nil {
			return errors.Wrap(err, "could not get aggregate and proof")
		}

		msg := &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof,
			Signature: specSig,
		}

		start := time.Now()
		endSubmission := r.metrics.StartBeaconSubmission()

		logger = logger.With(
			zap.Uint64s("signers", getPostConsensusSigners(r.GetState(), root)),
			fields.PreConsensusTime(r.metrics.GetPreConsensusTime()),
			fields.ConsensusTime(r.metrics.GetConsensusTime()),
			fields.PostConsensusTime(r.metrics.GetPostConsensusTime()),
			zap.String("block_root", hex.EncodeToString(msg.Message.Aggregate.Data.BeaconBlockRoot[:])),
		)
		if err := r.GetBeaconNode().SubmitSignedAggregateSelectionProof(msg); err != nil {
			r.metrics.RoleSubmissionFailed()
			logger.Error("❌ could not submit to Beacon chain reconstructed contribution and proof",
				fields.SubmissionTime(time.Since(start)),
				zap.Error(err))
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed aggregate")
		}

		endSubmission()
		r.metrics.EndDutyFullFlow(specqbft.Round(r.GetState().RunningInstance.State.Round))
		r.metrics.RoleSubmitted()

		logger.Debug("✅ successful submitted aggregate")
	}
	r.GetState().Finished = true

	return nil
}

func (r *AggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{genesisspectypes.SSZUint64(r.GetState().StartingDuty.Slot)}, genesisspectypes.DomainSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *AggregatorRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	aggregateAndProof, err := r.GetState().DecidedValue.GetAggregateAndProof()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get aggregate and proof")
	}

	return []ssz.HashRoot{aggregateAndProof}, genesisspectypes.DomainAggregateAndProof, nil
}

// executeDuty steps:
// 1) sign a partial selection proof and wait for 2f+1 partial sigs from peers
// 2) reconstruct selection proof and send SubmitAggregateSelectionProof to BN
// 3) start consensus on duty + aggregation data
// 4) Once consensus decides, sign partial aggregation data and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid SignedAggregateSubmitRequest sig to the BN
func (r *AggregatorRunner) executeDuty(logger *zap.Logger, duty *genesisspectypes.Duty) error {
	r.metrics.StartDutyFullFlow()
	r.metrics.StartPreConsensus()

	// sign selection proof
	msg, err := r.BaseRunner.signBeaconObject(r, genesisspectypes.SSZUint64(duty.Slot), duty.Slot, genesisspectypes.DomainSelectionProof)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}
	msgs := genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.SelectionProofPartialSig,
		Slot:     duty.Slot,
		Messages: []*genesisspectypes.PartialSignatureMessage{msg},
	}

	// sign msg
	signature, err := r.GetSigner().SignRoot(msgs, genesisspectypes.PartialSignatureType, r.GetShare().SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign PartialSignatureMessage for selection proof")
	}
	signedPartialMsg := &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: signature,
		Signer:    r.operatorSigner.GetOperatorID(),
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode selection proof pre-consensus signature msg")
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
		return errors.Wrap(err, "can't broadcast partial selection proof sig")
	}
	return nil
}

func (r *AggregatorRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *AggregatorRunner) GetNetwork() genesisspecssv.Network {
	return r.network
}

func (r *AggregatorRunner) GetBeaconNode() genesisspecssv.BeaconNode {
	return r.beacon
}

func (r *AggregatorRunner) GetShare() *spectypes.Share {
	return r.BaseRunner.Share
}

func (r *AggregatorRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *AggregatorRunner) GetValCheckF() genesisspecqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *AggregatorRunner) GetSigner() genesisspectypes.KeyManager {
	return r.signer
}

func (r *AggregatorRunner) GetOperatorSigner() types.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *AggregatorRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *AggregatorRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
// GetRoot returns the root used for signing and verification
func (r *AggregatorRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}
