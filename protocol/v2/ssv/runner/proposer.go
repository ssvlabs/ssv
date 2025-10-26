package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	blindutil "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/blind"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type ProposerRunner struct {
	BaseRunner *BaseRunner

	beacon              beacon.BeaconNode
	network             specqbft.Network
	signer              ekm.BeaconSigner
	operatorSigner      ssvtypes.OperatorSigner
	doppelgangerHandler DoppelgangerProvider
	valCheck            specqbft.ProposedValueCheckF
	measurements        measurementsStore
	graffiti            []byte

	// proposerDelay allows Operator to configure a delay to wait out before requesting Ethereum
	// block to propose if this Operator is proposer-duty Leader. This allows Operator to extract
	// higher MEV.
	proposerDelay time.Duration

	// cachedFullBlock holds the initially fetched full (non-blinded) block
	// for this duty on this operator, if any. Used so that the leader of the
	// decided QBFT round can submit the full block + blobs after signatures are
	// collected, while still proposing a blinded value during QBFT.
	cachedFullBlock *api.VersionedProposal
	// cachedBlindedBlockSSZ is a fingerprint of the cachedFullBlock, it is stored here
	// for efficient validation (so we re-use it instead of re-calculating).
	cachedBlindedBlockSSZ []byte
}

func NewProposerRunner(
	logger *zap.Logger,
	networkConfig *networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	doppelgangerHandler DoppelgangerProvider,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
	graffiti []byte,
	proposerDelay time.Duration,
) (Runner, error) {
	if len(share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &ProposerRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleProposer,
			NetworkConfig:      networkConfig,
			Share:              share,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},

		beacon:              beacon,
		network:             network,
		signer:              signer,
		operatorSigner:      operatorSigner,
		doppelgangerHandler: doppelgangerHandler,
		valCheck:            valCheck,
		measurements:        NewMeasurementsStore(),
		graffiti:            graffiti,

		proposerDelay: proposerDelay,
	}, nil
}

func (r *ProposerRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewDuty(ctx, logger, r, duty, quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *ProposerRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *ProposerRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_pre_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	hasQuorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing randao message: %w", err)
	}

	// signer must be same for all messages, at least 1 message must be present (this is validated prior)
	signer := signedMsg.Messages[0].Signer
	duty := r.GetState().StartingDuty.(*spectypes.ValidatorDuty)

	logger.Debug("🧩 got partial RANDAO signatures", zap.Uint64("signer", signer))

	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleProposer)

	// only 1 root, verified in expectedPreConsensusRootsAndDomain
	root := roots[0]

	// randao is relevant only for block proposals, no need to check type
	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return traces.Errorf(span, "got pre-consensus quorum but it has invalid signatures: %w", err)
	}
	logger.Debug(
		"🧩 reconstructed partial RANDAO signatures",
		zap.Uint64s("signers", getPreConsensusSigners(r.GetState(), root)),
		fields.PreConsensusTime(r.measurements.PreConsensusTime()),
	)

	// Sleep the remaining proposerDelay since slot start, ensuring on-time proposals even if duty began late.
	slotTime := r.BaseRunner.NetworkConfig.SlotStartTime(duty.Slot)
	proposeTime := slotTime.Add(r.proposerDelay)
	if timeLeft := time.Until(proposeTime); timeLeft > 0 {
		select {
		case <-time.After(timeLeft):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Fetch the block our operator will propose if it is a Leader (note, even if our operator
	// isn't leading the 1st QBFT round it might become a Leader in case of round change - hence
	// we are always fetching Ethereum block here just in case we need to propose it).
	start := time.Now()
	duty = r.GetState().StartingDuty.(*spectypes.ValidatorDuty)

	vBlk, _, err := r.GetBeaconNode().GetBeaconBlock(ctx, duty.Slot, r.graffiti, fullSig)
	if err != nil {
		const errMsg = "failed to get beacon block"

		logger.Error(errMsg,
			fields.PreConsensusTime(r.measurements.PreConsensusTime()),
			fields.BlockTime(time.Since(start)),
			zap.Error(err))
		return traces.Errorf(span, "%s: %w", errMsg, err)
	}

	// Log essentials about the retrieved block.
	logFields := []zap.Field{
		zap.String("version", vBlk.Version.String()),
		zap.Bool("blinded", vBlk.Blinded),
		zap.Duration("proposer_delay", r.proposerDelay),
		fields.Took(time.Since(start)),
	}

	blockHash, err := extractBlockHash(vBlk)
	if err != nil {
		logFields = append(logFields, zap.NamedError("blockHash_err", err))
	} else {
		logFields = append(logFields, fields.BlockHash(blockHash))
	}

	feeRecipient, err := vBlk.FeeRecipient()
	if err != nil {
		logFields = append(logFields, zap.NamedError("feeRecipient_err", err))
	} else {
		logFields = append(logFields, fields.FeeRecipient(feeRecipient[:]))
	}

	const eventMsg = "🧊 got beacon block proposal"
	logger.Info(eventMsg, logFields...)
	span.AddEvent(eventMsg, trace.WithAttributes(
		observability.BeaconBlockHashAttribute(blockHash),
		observability.BeaconBlockIsBlindedAttribute(vBlk.Blinded),
	))

	// Ensure we propose a blinded block in QBFT. If the beacon returned a full
	// block, convert it to blinded form by swapping the execution payload with
	// its header (+ cache the original block so we can submit it later).
	// Consensus value carries the blinded block SSZ.

	blindedVBlk, blindedMarshaler, err := blindutil.EnsureBlinded(vBlk)
	if err != nil {
		return traces.Errorf(span, "failed to blind full block: %w", err)
	}

	byts, err := blindedMarshaler.MarshalSSZ()
	if err != nil {
		return traces.Errorf(span, "could not marshal blinded beacon block: %w", err)
	}

	// Store the original block (we are only interested in full blocks) for later re-use
	// in the post-consensus phase.
	if !vBlk.Blinded {
		r.cachedFullBlock = vBlk
		r.cachedBlindedBlockSSZ = byts
	}

	input := &spectypes.ValidatorConsensusData{
		Duty:    *duty,
		Version: blindedVBlk.Version,
		DataSSZ: byts,
	}

	r.measurements.StartConsensus()

	if err := r.BaseRunner.decide(ctx, logger, r, duty.Slot, input); err != nil {
		return traces.Errorf(span, "can't start new duty runner instance for duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *ProposerRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_consensus"),
		trace.WithAttributes(
			observability.ValidatorMsgIDAttribute(signedMsg.SSVMessage.GetID()),
			observability.ValidatorMsgTypeAttribute(signedMsg.SSVMessage.GetType()),
			observability.RunnerRoleAttribute(signedMsg.SSVMessage.GetID().GetRoleType()),
		))
	defer span.End()

	span.AddEvent("checking if instance is decided")
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r.GetValCheckF(), signedMsg, &spectypes.ValidatorConsensusData{})
	if err != nil {
		return traces.Errorf(span, "failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		span.AddEvent("instance is not decided")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("instance is decided")
	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleProposer)

	r.measurements.StartPostConsensus()

	cd := decidedValue.(*spectypes.ValidatorConsensusData)
	span.SetAttributes(
		observability.BeaconSlotAttribute(cd.Duty.Slot),
		observability.ValidatorPublicKeyAttribute(cd.Duty.PubKey),
	)

	versionedBlock, blkRootToSign, err := cd.GetBlockData()
	if err != nil {
		return traces.Errorf(span, "could not get block data from consensus data: %w", err)
	}

	if versionedBlock.Blinded {
		span.AddEvent("decided has a blinded block")
	} else {
		span.AddEvent("decided has a vanilla block")
	}

	duty := r.BaseRunner.State.StartingDuty.(*spectypes.ValidatorDuty)
	if !r.doppelgangerHandler.CanSign(duty.ValidatorIndex) {
		logger.Warn("Signing not permitted due to Doppelganger protection", fields.ValidatorIndex(duty.ValidatorIndex))
		return nil
	}

	span.AddEvent("signing beacon object")
	msg, err := signBeaconObject(
		ctx,
		r,
		duty,
		blkRootToSign,
		cd.Duty.Slot,
		spectypes.DomainProposer,
	)
	if err != nil {
		return traces.Errorf(span, "failed signing block: %w", err)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     cd.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := postConsensusMsg.Encode()
	if err != nil {
		return traces.Errorf(span, "could not encode post consensus partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV partial signature message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign SSV partial signature message: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return fmt.Errorf("can't broadcast partial post consensus sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *ProposerRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error {
	return r.BaseRunner.OnTimeoutQBFT(ctx, logger, msg)
}

func (r *ProposerRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_post_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	hasQuorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing post consensus message: %w", err)
	}
	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// only 1 root, verified by expectedPostConsensusRootsAndDomain
	root := roots[0]

	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return traces.Errorf(span, "got post-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], sig)

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleProposer)

	logger.Debug("🧩 reconstructed partial post consensus signatures proposer",
		zap.Uint64s("signers", getPostConsensusProposerSigners(r.GetState(), root)),
		fields.PostConsensusTime(r.measurements.PostConsensusTime()),
		fields.QBFTRound(r.GetState().RunningInstance.State.Round))

	r.doppelgangerHandler.ReportQuorum(r.GetShare().ValidatorIndex)

	start := time.Now()

	logger = logger.With(
		fields.PreConsensusTime(r.measurements.PreConsensusTime()),
		fields.ConsensusTime(r.measurements.ConsensusTime()),
		fields.PostConsensusTime(r.measurements.PostConsensusTime()),
		fields.QBFTHeight(r.BaseRunner.QBFTController.Height),
		fields.QBFTRound(r.GetState().RunningInstance.State.Round),
	)

	// If this operator is the leader of the decided round and it originally
	// fetched a full (non-blinded) block, prefer submitting the full locally
	// cached block (including blobs for Deneb/Electra/Fulu) - but only if
	// the root of the decided block matches our locally cached block root.
	// Other operators will keep submitting the blinded variant.
	// TODO: should we send the block at all if we're not the leader? It's probably not effective but
	//		I left it for now to keep backwards compatibility.
	validatorConsensusData := &spectypes.ValidatorConsensusData{}
	err = validatorConsensusData.Decode(r.GetState().DecidedValue)
	if err != nil {
		return traces.Errorf(span, "could not decode decided validator consensus data: %w", err)
	}
	vBlk, _, err := validatorConsensusData.GetBlockData()
	if err != nil {
		return traces.Errorf(span, "could not get block data from consensus data: %w", err)
	}
	leaderID := r.GetState().RunningInstance.Proposer()
	if r.cachedFullBlock != nil && leaderID == r.operatorSigner.GetOperatorID() {
		if bytes.Equal(validatorConsensusData.DataSSZ, r.cachedBlindedBlockSSZ) {
			logger.Debug("leader will use the original full block for proposal submission")
			vBlk = r.cachedFullBlock
		} else {
			logger.Debug(
				"leader will use the decided block for proposal submission because decided block root hash doesn't match cached block root hash",
				zap.String("decided_block_ssz", hex.EncodeToString(validatorConsensusData.DataSSZ)),
				zap.String("cached_block_ssz", hex.EncodeToString(r.cachedBlindedBlockSSZ)),
			)
		}
	}

	loggerFields := []zap.Field{
		zap.String("version", vBlk.Version.String()),
		zap.Bool("blinded", vBlk.Blinded),
	}

	blockHash, err := extractBlockHash(vBlk)
	if err != nil {
		loggerFields = append(loggerFields, zap.NamedError("blockHash_err", err))
	} else {
		loggerFields = append(loggerFields, fields.BlockHash(blockHash))
	}

	logger = logger.With(loggerFields...)

	if err := r.GetBeaconNode().SubmitBeaconBlock(ctx, vBlk, specSig); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposer)

		const errMsg = "could not submit beacon block"
		logger.Error(errMsg,
			fields.SubmissionTime(time.Since(start)),
			zap.Error(err))
		return traces.Errorf(span, "%s: %w", errMsg, err)
	}

	const eventMsg = "✅ successfully submitted block proposal"
	span.AddEvent(eventMsg, trace.WithAttributes(
		observability.BeaconSlotAttribute(r.BaseRunner.State.StartingDuty.DutySlot()),
		observability.DutyRoundAttribute(r.BaseRunner.State.RunningInstance.State.Round),
		observability.BeaconBlockHashAttribute(blockHash),
		observability.BeaconBlockIsBlindedAttribute(vBlk.Blinded),
	))
	logger.Info(eventMsg,
		fields.QBFTHeight(r.BaseRunner.QBFTController.Height),
		fields.QBFTRound(r.GetState().RunningInstance.State.Round),
		fields.Took(time.Since(start)),
		fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
		fields.TotalDutyTime(r.measurements.TotalDutyTime()))

	r.GetState().Finished = true
	r.measurements.EndDutyFlow()

	recordDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.BNRoleProposer, r.GetState().RunningInstance.State.Round)
	recordSuccessfulSubmission(
		ctx,
		1,
		r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot()),
		spectypes.BNRoleProposer,
	)

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *ProposerRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot())
	return []ssz.HashRoot{spectypes.SSZUint64(epoch)}, spectypes.DomainRandao, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ProposerRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	validatorConsensusData := &spectypes.ValidatorConsensusData{}
	err := validatorConsensusData.Decode(r.GetState().DecidedValue)
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not decode consensus data")
	}

	_, signedRoot, err := validatorConsensusData.GetBlockData()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get block data")
	}
	return []ssz.HashRoot{signedRoot}, spectypes.DomainProposer, nil
}

// executeDuty steps:
// 1) sign a partial randao sig and wait for 2f+1 partial sigs from peers
// 2) reconstruct randao and send GetBeaconBlock to BN
// 3) start consensus on duty + block data
// 4) Once consensus decides, sign partial block and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid block sig to the BN
func (r *ProposerRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	_, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.execute_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	r.measurements.StartDutyFlow()
	r.measurements.StartPreConsensus()

	proposerDuty := duty.(*spectypes.ValidatorDuty)
	if !r.doppelgangerHandler.CanSign(proposerDuty.ValidatorIndex) {
		logger.Warn("Signing not permitted due to Doppelganger protection", fields.ValidatorIndex(proposerDuty.ValidatorIndex))
		return nil
	}

	// reset the cached original block at the beginning of a new duty
	r.cachedFullBlock = nil
	r.cachedBlindedBlockSSZ = nil

	// sign partial randao
	span.AddEvent("signing beacon object")
	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	msg, err := signBeaconObject(
		ctx,
		r,
		proposerDuty,
		spectypes.SSZUint64(epoch),
		duty.DutySlot(),
		spectypes.DomainRandao,
	)
	if err != nil {
		return traces.Errorf(span, "could not sign randao: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return traces.Errorf(span, "could not encode randao partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return traces.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting signed SSV message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return traces.Errorf(span, "can't broadcast partial randao sig: %w", err)
	}

	logger.Debug("🔏 signed & broadcasted partial RANDAO signature")

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *ProposerRunner) HasRunningQBFTInstance() bool {
	return r.BaseRunner.HasRunningQBFTInstance()
}

func (r *ProposerRunner) HasAcceptedProposalForCurrentRound() bool {
	return r.BaseRunner.HasAcceptedProposalForCurrentRound()
}

func (r *ProposerRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	return r.BaseRunner.GetShares()
}

func (r *ProposerRunner) GetRole() spectypes.RunnerRole {
	return r.BaseRunner.GetRole()
}

func (r *ProposerRunner) GetLastHeight() specqbft.Height {
	return r.BaseRunner.GetLastHeight()
}

func (r *ProposerRunner) GetLastRound() specqbft.Round {
	return r.BaseRunner.GetLastRound()
}

func (r *ProposerRunner) GetStateRoot() ([32]byte, error) {
	return r.BaseRunner.GetStateRoot()
}

func (r *ProposerRunner) SetTimeoutFunc(fn TimeoutF) {
	r.BaseRunner.SetTimeoutFunc(fn)
}

func (r *ProposerRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *ProposerRunner) GetNetworkConfig() *networkconfig.Network {
	return r.BaseRunner.NetworkConfig
}

func (r *ProposerRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *ProposerRunner) GetShare() *spectypes.Share {
	// TODO better solution for this
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *ProposerRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *ProposerRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *ProposerRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *ProposerRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *ProposerRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *ProposerRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *ProposerRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode ProposerRunner")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// extractBlockHash extracts the block hash from a VersionedProposal.
// It handles both regular and blinded blocks across all supported versions.
func extractBlockHash(vBlk *api.VersionedProposal) (phase0.Hash32, error) {
	if vBlk == nil {
		return phase0.Hash32{}, fmt.Errorf("block is nil")
	}

	switch vBlk.Version {
	case spec.DataVersionCapella:
		if vBlk.Blinded {
			if vBlk.CapellaBlinded == nil || vBlk.CapellaBlinded.Body == nil ||
				vBlk.CapellaBlinded.Body.ExecutionPayloadHeader == nil {
				return phase0.Hash32{}, fmt.Errorf("capella blinded block data missing")
			}
			return vBlk.CapellaBlinded.Body.ExecutionPayloadHeader.BlockHash, nil
		}
		if vBlk.Capella == nil || vBlk.Capella.Body == nil ||
			vBlk.Capella.Body.ExecutionPayload == nil {
			return phase0.Hash32{}, fmt.Errorf("capella block data missing")
		}
		return vBlk.Capella.Body.ExecutionPayload.BlockHash, nil

	case spec.DataVersionDeneb:
		if vBlk.Blinded {
			if vBlk.DenebBlinded == nil || vBlk.DenebBlinded.Body == nil ||
				vBlk.DenebBlinded.Body.ExecutionPayloadHeader == nil {
				return phase0.Hash32{}, fmt.Errorf("deneb blinded block data missing")
			}
			return vBlk.DenebBlinded.Body.ExecutionPayloadHeader.BlockHash, nil
		}
		if vBlk.Deneb == nil || vBlk.Deneb.Block == nil || vBlk.Deneb.Block.Body == nil ||
			vBlk.Deneb.Block.Body.ExecutionPayload == nil {
			return phase0.Hash32{}, fmt.Errorf("deneb block data missing")
		}
		return vBlk.Deneb.Block.Body.ExecutionPayload.BlockHash, nil

	case spec.DataVersionElectra:
		if vBlk.Blinded {
			if vBlk.ElectraBlinded == nil || vBlk.ElectraBlinded.Body == nil ||
				vBlk.ElectraBlinded.Body.ExecutionPayloadHeader == nil {
				return phase0.Hash32{}, fmt.Errorf("electra blinded block data missing")
			}
			return vBlk.ElectraBlinded.Body.ExecutionPayloadHeader.BlockHash, nil
		}
		if vBlk.Electra == nil || vBlk.Electra.Block == nil || vBlk.Electra.Block.Body == nil ||
			vBlk.Electra.Block.Body.ExecutionPayload == nil {
			return phase0.Hash32{}, fmt.Errorf("electra block data missing")
		}
		return vBlk.Electra.Block.Body.ExecutionPayload.BlockHash, nil

	case spec.DataVersionFulu:
		if vBlk.Blinded {
			if vBlk.FuluBlinded == nil || vBlk.FuluBlinded.Body == nil ||
				vBlk.FuluBlinded.Body.ExecutionPayloadHeader == nil {
				return phase0.Hash32{}, fmt.Errorf("fulu blinded block data missing")
			}
			return vBlk.FuluBlinded.Body.ExecutionPayloadHeader.BlockHash, nil
		}
		if vBlk.Fulu == nil || vBlk.Fulu.Block == nil || vBlk.Fulu.Block.Body == nil ||
			vBlk.Fulu.Block.Body.ExecutionPayload == nil {
			return phase0.Hash32{}, fmt.Errorf("fulu block data missing")
		}
		return vBlk.Fulu.Block.Body.ExecutionPayload.BlockHash, nil

	default:
		return phase0.Hash32{}, fmt.Errorf("unsupported block version %d", vBlk.Version)
	}
}
