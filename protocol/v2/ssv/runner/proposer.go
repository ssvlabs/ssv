package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	blindutil "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/blind"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

type ProposerRunner struct {
	*BaseRunner

	beacon              beacon.BeaconNode
	network             protocolp2p.Network
	signer              ekm.BeaconSigner
	operatorSigner      ssvtypes.OperatorSigner
	doppelgangerHandler DoppelgangerProvider
	measurements        *dutyMeasurements
	graffiti            []byte

	// ValCheck is used to validate the qbft-value(s) proposed by other Operators.
	ValCheck ssv.ValueChecker

	// proposerDelay allows Operator to configure a delay to wait out before requesting Ethereum
	// block to propose if this Operator is proposer-duty Leader. This allows Operator to extract
	// higher MEV. proposerDelayEPBS is its post-Gloas counterpart (see proposerDelayForSlot).
	proposerDelay     time.Duration
	proposerDelayEPBS time.Duration

	// cachedFullBlock holds the initially fetched full (non-blinded) block
	// for this duty on this operator, if any. Used so that the leader of the
	// decided QBFT round can submit the full block + blobs after signatures are
	// collected, while still proposing a blinded value during QBFT.
	cachedFullBlock *api.VersionedProposal
	// cachedBlindedBlockSSZ is a fingerprint of the cachedFullBlock, it is stored here
	// for efficient validation (so we re-use it instead of re-calculating).
	cachedBlindedBlockSSZ []byte

	// proposedBlockRoots records this operator's §4-decided block root per slot so the §6 envelope
	// runner and its value-check can read it (SIP #94 §6); nil pre-Gloas.
	proposedBlockRoots *ssv.ProposedBlockRoots

	// startEnvelopeDuty starts the §6 envelope-signing duty for a slot, called after a self-build §4
	// block is published. Injected by the controller; nil pre-Gloas / when the envelope runner is absent.
	// It must dispatch async with a node-scoped context: the caller runs on the proposer's post-consensus
	// path, whose context is canceled once the block duty ends.
	startEnvelopeDuty func(slot phase0.Slot)

	// builders is the cluster's direct-builder config, resolved once at construction (issue #2962, phase 2):
	// the produceBlockV4 POST body is assembled from it plus the per-slot reconstructed auths. Not
	// Configured() -> the enshrined GET.
	builders gloas.ResolvedBuilderConfig
	// requestAuthCache holds the per-slot reconstructed builder auths this operator attaches to the
	// produceBlockV4 POST. Shared with the §5 dispatcher that writes it; nil pre-Gloas / no overlay.
	requestAuthCache *ssv.RequestAuthCache
	// gloasProducedRoot / gloasBuilderURL record this operator's own §4 produce output for the slot: the
	// produced block root and any winning builder URL. At publish, Eth-Builder-Url is echoed only when the
	// decided block matches gloasProducedRoot (owner-match — see decidedBuilderURL).
	gloasProducedRoot [32]byte
	gloasBuilderURL   string
}

// ProposerRunnerOptions bundles all dependencies required by NewProposerRunner.
type ProposerRunnerOptions struct {
	BaseRunnerOptions

	QBFTController      *controller.Controller
	DoppelgangerHandler DoppelgangerProvider
	ValCheck            ssv.ValueChecker
	HighestDecidedSlot  phase0.Slot
	Graffiti            []byte
	// ProposerDelay allows Operator to configure a delay to wait out before requesting Ethereum
	// block to propose if this Operator is proposer-duty Leader. This allows Operator to extract
	// higher MEV. ProposerDelayEPBS is its post-Gloas counterpart, applied from the Gloas fork on.
	ProposerDelay     time.Duration
	ProposerDelayEPBS time.Duration

	// ProposedBlockRoots is the shared store the proposer records its §4-decided block root into for the
	// §6 envelope runner to read. Optional (nil pre-Gloas / when the envelope runner is absent).
	ProposedBlockRoots *ssv.ProposedBlockRoots

	// StartEnvelopeDuty starts the §6 envelope-signing duty for a slot; called after a self-build §4
	// block is published. Must dispatch async with a node-scoped context (see startEnvelopeDuty). Optional.
	StartEnvelopeDuty func(slot phase0.Slot)

	// Builders / RequestAuthCache feed the phase-2 produceBlockV4 POST body (issue #2962). Optional
	// (empty / nil pre-Gloas or when the direct-builder overlay is unconfigured).
	Builders         gloas.BuilderConfig
	RequestAuthCache *ssv.RequestAuthCache
}

func NewProposerRunner(opts ProposerRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}

	// Decode/resolve the builder config once here (once per validator), so the §4 produce path reads
	// pre-decoded values instead of re-parsing config every proposal. Startup already validated it.
	builders, err := gloas.ResolveBuilderConfig(opts.Builders)
	if err != nil {
		return nil, fmt.Errorf("resolve builder config: %w", err)
	}

	return &ProposerRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleProposer,
			NetworkConfig:      opts.NetworkConfig,
			Share:              opts.Share,
			QBFTController:     opts.QBFTController,
			highestDecidedSlot: opts.HighestDecidedSlot,
		},

		beacon:              opts.Beacon,
		network:             opts.Network,
		signer:              opts.Signer,
		operatorSigner:      opts.OperatorSigner,
		doppelgangerHandler: opts.DoppelgangerHandler,
		ValCheck:            opts.ValCheck,
		measurements:        newMeasurementsStore(),
		graffiti:            opts.Graffiti,

		proposerDelay:      opts.ProposerDelay,
		proposerDelayEPBS:  opts.ProposerDelayEPBS,
		proposedBlockRoots: opts.ProposedBlockRoots,
		startEnvelopeDuty:  opts.StartEnvelopeDuty,
		builders:           builders,
		requestAuthCache:   opts.RequestAuthCache,
	}, nil
}

func (r *ProposerRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	return r.baseStartNewDuty(ctx, logger, r, validatorDuty, quorum)
}

func (r *ProposerRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// Since we are re-using the same runner for different duties, ErrRunningDutySucceeded error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing randao message: %w", err)
	}
	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here. The quorum above fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleProposer)

	// only 1 root, verified in expectedPreConsensusRootsAndDomain
	root := roots[0]

	fullSig, err := r.State.ReconstructBeaconSig(r.State.PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.FallBackAndVerifyEachSignature(r.State.PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err)
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	// Sleep the remaining proposerDelay since slot start, ensuring on-time proposals even if duty began late.
	if timeLeft := r.remainingProposerDelay(duty.Slot, time.Now()); timeLeft > 0 {
		select {
		case <-time.After(timeLeft):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	waitedOutProposerDelayEvent := fmt.Sprintf("waited out proposer delay of %dms", r.proposerDelayForSlot(duty.Slot).Milliseconds())
	logger.Debug(waitedOutProposerDelayEvent)
	span.AddEvent(waitedOutProposerDelayEvent)

	duty, err = r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	// Fetch the block our operator will propose if it is a Leader (note, even if our operator
	// isn't leading the 1st QBFT round it might become a Leader in case of round change - hence
	// we are always fetching Ethereum block here just in case we need to propose it).
	var input *spectypes.ProposerConsensusData
	if r.NetworkConfig.IsGloasAtSlot(duty.Slot) {
		input, err = r.gloasProposalInput(ctx, logger, duty, fullSig)
		if err != nil {
			return err
		}
	} else {
		start := time.Now()
		vBlk, _, err := r.GetBeaconNode().GetBeaconBlock(ctx, duty.Slot, r.graffiti, fullSig)
		if err != nil {
			return fmt.Errorf("get beacon block: %w", err)
		}
		// Log essentials about the retrieved block.
		logFields, proposalTraceAttrs := proposalCommonFields(vBlk)
		logFields = append(
			logFields,
			zap.Duration("proposer_delay", r.proposerDelay),
			fields.Took(time.Since(start)),
		)

		feeRecipient, err := vBlk.FeeRecipient()
		if err != nil {
			logFields = append(logFields, zap.NamedError("feeRecipient_err", err))
		} else {
			logFields = append(logFields, fields.FeeRecipient(feeRecipient[:]))
		}
		const eventMsg = "🧊 got beacon block proposal"
		logger.Info(eventMsg, logFields...)
		span.AddEvent(eventMsg, trace.WithAttributes(proposalTraceAttrs...))

		// Ensure we propose a blinded block in QBFT. If the beacon returned a full
		// block, convert it to blinded form by swapping the execution payload with
		// its header (+ cache the original block so we can submit it later).
		// Consensus value carries the blinded block SSZ.
		blindedVBlk, blindedMarshaler, err := blindutil.EnsureBlinded(vBlk)
		if err != nil {
			return fmt.Errorf("failed to blind full block: %w", err)
		}

		byts, err := blindedMarshaler.MarshalSSZ()
		if err != nil {
			return fmt.Errorf("could not marshal blinded beacon block: %w", err)
		}

		// Store the original block (we are only interested in full blocks) for later re-use
		// in the post-consensus phase.
		if !vBlk.Blinded {
			r.cachedFullBlock = vBlk
			r.cachedBlindedBlockSSZ = byts
		}

		input = &spectypes.ProposerConsensusData{
			Duty:    *duty,
			Version: blindedVBlk.Version,
			DataSSZ: byts,
		}
	}

	r.measurements.StartConsensus()
	if err := r.decide(ctx, logger, duty.Slot, input, r.ValCheck); err != nil {
		return fmt.Errorf("qbft-decide: %w", err)
	}

	return nil
}

// gloasProposalInput fetches the Gloas (ePBS) block this operator would propose and wraps it as the
// QBFT consensus value. The block carries only the execution-payload bid (the payload ships in the §6
// envelope), so unlike the pre-Gloas path there is no blinding — the marshaled block is the QBFT
// consensus value (DataSSZ) directly.
func (r *ProposerRunner) gloasProposalInput(ctx context.Context, logger *zap.Logger, duty *spectypes.ValidatorDuty, randaoReveal []byte) (*spectypes.ProposerConsensusData, error) {
	start := time.Now()
	builderConfig := r.gloasBuilderConfig(ctx, duty.Slot)
	block, builderURL, err := r.GetBeaconNode().GetGloasBeaconBlock(ctx, duty.Slot, r.graffiti, randaoReveal, builderConfig)
	if err != nil {
		return nil, fmt.Errorf("get gloas beacon block: %w", err)
	}

	// Remember this operator's own produce output so the §4 publish echoes Eth-Builder-Url only when the
	// decided block is this operator's own (owner-match — see decidedBuilderURL).
	if root, rootErr := block.HashTreeRoot(); rootErr == nil {
		r.gloasProducedRoot, r.gloasBuilderURL = root, builderURL
	}

	byts, err := block.MarshalSSZ()
	if err != nil {
		return nil, fmt.Errorf("could not marshal gloas beacon block: %w", err)
	}

	logFields := []zap.Field{
		fields.Slot(duty.Slot),
		zap.Duration("proposer_delay", r.proposerDelayForSlot(duty.Slot)),
		fields.Took(time.Since(start)),
	}
	if bid := block.Body.SignedExecutionPayloadBid; bid != nil && bid.Message != nil {
		logFields = append(logFields, fields.FeeRecipient(bid.Message.FeeRecipient[:]))
	}
	const eventMsg = "🧊 got gloas beacon block proposal"
	logger.Info(eventMsg, logFields...)
	trace.SpanFromContext(ctx).AddEvent(eventMsg)

	return &spectypes.ProposerConsensusData{
		Duty:    *duty,
		Version: networkconfig.DataVersionGloas,
		DataSSZ: byts,
	}, nil
}

// gloasBuilderConfig assembles the produceBlockV4 POST body from the cluster's direct-builder config and
// the per-slot reconstructed auths (beacon-APIs#630), or nil when nothing is configured (→ the enshrined
// GET). Builders whose auth missed quorum this slot are omitted and counted for the E1 auth-unavailable
// signal; the top-level p2p knobs are always carried. The goclient falls back to GET per beacon node that
// predates #630.
func (r *ProposerRunner) gloasBuilderConfig(ctx context.Context, slot phase0.Slot) *gloas.ProduceBuilderConfig {
	if !r.builders.Configured() {
		return nil
	}
	var auths map[string]*gloas.SignedBuilderRequestAuth
	if r.requestAuthCache != nil {
		auths = r.requestAuthCache.Get(slot)
	}
	cfg, authUnavailable := gloas.BuildProduceConfig(r.builders, auths)
	if authUnavailable > 0 {
		recordProposalAuthUnavailable(ctx, authUnavailable)
	}
	return &cfg
}

func (r *ProposerRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("processing QBFT consensus msg")
	decided, decidedValue, err := r.baseConsensusMsgProcessing(ctx, logger, r.ValCheck.CheckValue, signedMsg, &spectypes.ProposerConsensusData{})
	if err != nil {
		return fmt.Errorf("failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleProposer)

	cd := decidedValue.(*spectypes.ProposerConsensusData)
	span.SetAttributes(
		observability.BeaconSlotAttribute(cd.Duty.Slot),
		observability.ValidatorPublicKeyAttribute(cd.Duty.PubKey),
	)

	var blkRootToSign ssz.HashRoot
	if r.NetworkConfig.IsGloasAtSlot(cd.Duty.Slot) {
		// Gloas blocks have no spectypes block version; decode the node-side block, which doubles as
		// the ssz.HashRoot to sign.
		block, decErr := gloas.DecodeBeaconBlock(cd.DataSSZ)
		if decErr != nil {
			return fmt.Errorf("could not decode gloas block from consensus data: %w", decErr)
		}
		blkRootToSign = block
		if err := r.recordDecidedBlockRoot(cd.Duty.Slot, block); err != nil {
			return err
		}
		span.AddEvent("decided has a gloas block")
	} else {
		versionedBlock, signingRoot, err := cd.GetBlockData()
		if err != nil {
			return fmt.Errorf("could not get block data from consensus data: %w", err)
		}
		blkRootToSign = signingRoot
		if versionedBlock.Blinded {
			span.AddEvent("decided has a blinded block")
		} else {
			span.AddEvent("decided has a vanilla block")
		}
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}
	if !r.doppelgangerHandler.CanSign(duty.ValidatorIndex) {
		logger.Warn("Signing not permitted due to Doppelganger protection", fields.ValidatorIndex(duty.ValidatorIndex))
		return nil
	}

	span.AddEvent("signing beacon object")
	msg, err := signBeaconObject(
		ctx,
		r,
		r.NetworkConfig,
		duty,
		blkRootToSign,
		cd.Duty.Slot,
		spectypes.DomainProposer,
	)
	if err != nil {
		return fmt.Errorf("failed signing block: %w", err)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     cd.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	r.measurements.StartPostConsensus()
	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.signAndBroadcastPostConsensusMsg(r.GetNetwork(), r.operatorSigner, r.GetShare().ValidatorPubKey[:], postConsensusMsg); err != nil {
		return fmt.Errorf("can't broadcast partial post consensus sig: %w", err)
	}
	const broadcastedPostConsensusMsgEvent = "broadcasted post-consensus partial signature message"
	logger.Debug(broadcastedPostConsensusMsgEvent)
	span.AddEvent(broadcastedPostConsensusMsgEvent)

	return nil
}

func (r *ProposerRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePostConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// Since we are re-using the same runner for different duties, ErrRunningDutySucceeded error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing post consensus message: %w", err)
	}
	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here. The quorum above fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleProposer)

	// only 1 root, verified by expectedPostConsensusRootsAndDomain
	root := roots[0]

	sig, err := r.State.ReconstructBeaconSig(r.State.PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.FallBackAndVerifyEachSignature(r.State.PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got post-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], sig)

	r.doppelgangerHandler.ReportQuorum(r.GetShare().ValidatorIndex)

	const submittingBlockProposalEvent = "submitting block proposal"
	span.AddEvent(submittingBlockProposalEvent)
	logger.Info(submittingBlockProposalEvent)

	// If this operator is the leader of the decided round and it originally
	// fetched a full (non-blinded) block, prefer submitting the full locally
	// cached block (including blobs for Deneb/Electra/Fulu) - but only if
	// the root of the decided block matches our locally cached block root.
	// Other operators will keep submitting the blinded variant.
	// TODO: should we send the block at all if we're not the leader? It's probably not effective but
	//		I left it for now to keep backwards compatibility.
	validatorConsensusData := &spectypes.ProposerConsensusData{}
	err = validatorConsensusData.Decode(r.State.DecidedValue)
	if err != nil {
		return fmt.Errorf("could not decode decided validator consensus data: %w", err)
	}

	if r.NetworkConfig.IsGloasAtSlot(validatorConsensusData.Duty.Slot) {
		return r.submitGloasProposal(ctx, logger, span, validatorConsensusData, specSig)
	}

	vBlk, _, err := validatorConsensusData.GetBlockData()
	if err != nil {
		return fmt.Errorf("could not get block data from consensus data: %w", err)
	}
	leaderID := r.State.RunningInstance.Proposer()
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

	loggerFields, proposalTraceAttrs := proposalCommonFields(vBlk)
	logger = logger.With(loggerFields...)

	start := time.Now()
	if err := r.GetBeaconNode().SubmitBeaconBlock(ctx, vBlk, specSig); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposer)
		const errMsg = "could not submit beacon block"
		logger.Error(errMsg, fields.Slot(validatorConsensusData.Duty.Slot), zap.Error(err))
		return fmt.Errorf("%s: %w", errMsg, err)
	}
	return r.finishSubmittedProposal(ctx, logger, span, start, proposalTraceAttrs)
}

// finishSubmittedProposal records metrics, marks the duty succeeded, and logs after a proposal block
// has been submitted to the beacon node. submittedAt is when the submission started (for the Took
// metric); proposalTraceAttrs are block-specific span attributes (nil for Gloas).
func (r *ProposerRunner) finishSubmittedProposal(ctx context.Context, logger *zap.Logger, span trace.Span, submittedAt time.Time, proposalTraceAttrs []attribute.KeyValue) error {
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return fmt.Errorf("current duty slot: %w", err)
	}
	recordSuccessfulSubmission(ctx, 1, r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot), spectypes.BNRoleProposer)
	const submittedBlockProposalEvent = "✅ successfully submitted block proposal"
	submittedAttrs := append([]attribute.KeyValue{
		observability.BeaconSlotAttribute(currentDutySlot),
		observability.DutyRoundAttribute(r.State.RunningInstance.State.Round),
	}, proposalTraceAttrs...)
	span.AddEvent(submittedBlockProposalEvent, trace.WithAttributes(submittedAttrs...))
	logger.Info(submittedBlockProposalEvent, fields.Took(time.Since(submittedAt)))

	r.markDutySucceeded()
	r.measurements.EndDutyFlow()
	recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.RoleProposer, r.State.RunningInstance.State.Round)
	const dutyFinishedEvent = "✔️successfully finished duty processing"
	logger.Info(dutyFinishedEvent,
		fields.PreConsensusTime(r.measurements.PreConsensusTime()),
		fields.ConsensusTime(r.measurements.ConsensusTime()),
		fields.ConsensusRounds(uint64(r.State.RunningInstance.State.Round)),
		fields.PostConsensusTime(r.measurements.PostConsensusTime()),
		fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
		fields.TotalDutyTime(r.measurements.TotalDutyTime()),
	)
	span.AddEvent(dutyFinishedEvent)

	return nil
}

// submitGloasProposal publishes the decided Gloas (ePBS) block, then on the self-build path starts the §6
// envelope-signing duty. Every operator submits the decided block — the ePBS block is bid-only so all hold
// it, keeping the pre-Gloas all-submit redundancy. That relies on the BN deduping duplicate submissions by
// root (battle-tested pre-Gloas; still to be confirmed against a real Gloas BN). The block is decoded up
// front because every operator needs its bid for the self-build check; the envelope trigger fires on every
// operator and dispatches async, so it never delays the block.
func (r *ProposerRunner) submitGloasProposal(ctx context.Context, logger *zap.Logger, span trace.Span, cd *spectypes.ProposerConsensusData, sig phase0.BLSSignature) error {
	block, err := gloas.DecodeBeaconBlock(cd.DataSSZ)
	if err != nil {
		return fmt.Errorf("could not decode decided gloas block: %w", err)
	}

	logger.Debug("decided gloas block build source",
		fields.Slot(cd.Duty.Slot),
		zap.Bool("self_build", selfBuild(block)))

	var finishErr error
	start := time.Now()
	signedBlock := &gloas.SignedBeaconBlock{Message: block, Signature: sig}
	if err := r.GetBeaconNode().SubmitGloasBeaconBlock(ctx, signedBlock, r.decidedBuilderURL(block)); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposer)
		const errMsg = "could not submit gloas beacon block"
		logger.Error(errMsg, fields.Slot(cd.Duty.Slot), zap.Error(err))
		finishErr = fmt.Errorf("%s: %w", errMsg, err)
	} else {
		recordProposalBuildSource(ctx, gloasBuildSource(block))
		finishErr = r.finishSubmittedProposal(ctx, logger, span, start, nil)
	}

	r.triggerEnvelopeIfSelfBuild(block, cd.Duty.Slot)
	return finishErr
}

// triggerEnvelopeIfSelfBuild starts the §6 envelope-signing duty for the slot when the decided block is
// self-build — only then does the SSV cluster sign the envelope (external builders sign their own). The
// starter dispatches async (see startEnvelopeDuty); it no-ops when the starter is unset (e.g. in tests).
func (r *ProposerRunner) triggerEnvelopeIfSelfBuild(block *gloas.BeaconBlock, slot phase0.Slot) {
	if r.startEnvelopeDuty == nil || !selfBuild(block) {
		return
	}
	r.startEnvelopeDuty(slot)
}

// decidedBuilderURL returns the Eth-Builder-Url to echo on publish: this operator's own produce
// Eth-Builder-Url, but only when the decided block is the one this operator produced (owner-match). A
// follower publishing another operator's decided block echoes nothing — its beacon node did not solicit
// that bid and holds no forwarding target for it.
func (r *ProposerRunner) decidedBuilderURL(block *gloas.BeaconBlock) string {
	if r.gloasBuilderURL == "" {
		return ""
	}
	root, err := block.HashTreeRoot()
	if err != nil || root != r.gloasProducedRoot {
		return ""
	}
	return r.gloasBuilderURL
}

// selfBuild reports whether the decided Gloas block commits to a self-built payload
// (BUILDER_INDEX_SELF_BUILD) rather than an external builder's bid.
func selfBuild(block *gloas.BeaconBlock) bool {
	bid := block.Body.SignedExecutionPayloadBid
	return bid != nil && bid.Message != nil && bid.Message.BuilderIndex == gloas.BuilderIndexSelfBuild
}

// gloasBuildSource classifies a decided Gloas block for the build-source telemetry (issue #2962 E1).
func gloasBuildSource(block *gloas.BeaconBlock) proposalBuildSource {
	if selfBuild(block) {
		return buildSourceLocal
	}
	return buildSourceBuilder
}

// recordDecidedBlockRoot stores the §4-decided block's root for the §6 envelope runner and its
// value-check to read (SIP #94 §6). No-op when no envelope runner shares the store.
func (r *ProposerRunner) recordDecidedBlockRoot(slot phase0.Slot, block *gloas.BeaconBlock) error {
	if r.proposedBlockRoots == nil {
		return nil
	}
	root, err := block.HashTreeRoot()
	if err != nil {
		return fmt.Errorf("hash tree root of decided gloas block: %w", err)
	}
	r.proposedBlockRoots.Set(slot, phase0.Root(root))
	return nil
}

func (r *ProposerRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("current duty slot: %w", err)
	}
	epoch := r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot)
	return []ssz.HashRoot{spectypes.SSZUint64(epoch)}, spectypes.DomainRandao, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *ProposerRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	validatorConsensusData := &spectypes.ProposerConsensusData{}
	err := validatorConsensusData.Decode(r.State.DecidedValue)
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("could not decode consensus data: %w", err)
	}

	var signedRoot ssz.HashRoot
	if r.NetworkConfig.IsGloasAtSlot(validatorConsensusData.Duty.Slot) {
		block, decErr := gloas.DecodeBeaconBlock(validatorConsensusData.DataSSZ)
		if decErr != nil {
			return nil, phase0.DomainType{}, fmt.Errorf("could not decode gloas block: %w", decErr)
		}
		signedRoot = block
	} else {
		_, root, bdErr := validatorConsensusData.GetBlockData()
		if bdErr != nil {
			return nil, phase0.DomainType{}, fmt.Errorf("could not get block data: %w", bdErr)
		}
		signedRoot = root
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
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	r.measurements.StartDutyFlow()

	proposerDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	if !r.doppelgangerHandler.CanSign(proposerDuty.ValidatorIndex) {
		logger.Warn("Signing not permitted due to Doppelganger protection", fields.ValidatorIndex(proposerDuty.ValidatorIndex))
		return nil
	}

	// reset the cached original block at the beginning of a new duty
	r.cachedFullBlock = nil
	r.cachedBlindedBlockSSZ = nil

	// sign partial randao
	span.AddEvent("signing beacon object")
	epoch := r.NetworkConfig.EstimatedEpochAtSlot(duty.DutySlot())
	msg, err := signBeaconObject(
		ctx,
		r,
		r.NetworkConfig,
		proposerDuty,
		spectypes.SSZUint64(epoch),
		proposerDuty.DutySlot(),
		spectypes.DomainRandao,
	)
	if err != nil {
		return fmt.Errorf("could not sign randao: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     proposerDuty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	logger.Debug("signing and broadcasting randao partial sig", fields.Slot(proposerDuty.DutySlot()))

	r.measurements.StartPreConsensus()
	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey[:], msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast randao partial sig: %w", err)
	}

	return nil
}

// proposerDelayForSlot returns the fork-appropriate proposer delay: proposerDelayEPBS from the Gloas
// fork on, proposerDelay before it. They are separate knobs because ePBS retimes the proposal deadline
// (slot quarters), so their safe ranges differ.
func (r *ProposerRunner) proposerDelayForSlot(slot phase0.Slot) time.Duration {
	if r.NetworkConfig.IsGloasAtSlot(slot) {
		return r.proposerDelayEPBS
	}
	return r.proposerDelay
}

func (r *ProposerRunner) remainingProposerDelay(slot phase0.Slot, now time.Time) time.Duration {
	slotTime := r.NetworkConfig.SlotStartTime(slot)
	proposeTime := slotTime.Add(r.proposerDelayForSlot(slot))
	if wait := proposeTime.Sub(now); wait > 0 {
		return wait
	}
	return 0
}

func (r *ProposerRunner) GetNetwork() protocolp2p.Network {
	return r.network
}

func (r *ProposerRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *ProposerRunner) GetShare() *spectypes.Share {
	// TODO better solution for this
	for _, share := range r.Share {
		return share
	}
	return nil
}

func (r *ProposerRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *ProposerRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *ProposerRunner) MarshalJSON() ([]byte, error) {
	return marshalRunnerStateJSON(r.BaseRunner)
}

func (r *ProposerRunner) UnmarshalJSON(data []byte) error {
	br, err := unmarshalRunnerStateJSON(data)
	if err != nil {
		return err
	}
	r.BaseRunner = br
	r.ValCheck = nil
	return nil
}

// Encode returns the encoded struct in bytes or error
func (r *ProposerRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *ProposerRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

// GetRoot returns the root used for signing and verification
func (r *ProposerRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode ProposerRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

type executionInfo struct {
	BlockHash   phase0.Hash32
	ParentHash  phase0.Hash32
	BlockNumber uint64
}

// extractExecutionInfo extracts execution-layer info (hashes and block number) from a VersionedProposal.
// It handles both regular and blinded blocks across all supported versions.
func extractExecutionInfo(vBlk *api.VersionedProposal) (executionInfo, error) {
	if vBlk == nil {
		return executionInfo{}, fmt.Errorf("block is nil")
	}

	switch vBlk.Version {
	case spec.DataVersionCapella:
		if vBlk.Blinded {
			if vBlk.CapellaBlinded == nil || vBlk.CapellaBlinded.Body == nil ||
				vBlk.CapellaBlinded.Body.ExecutionPayloadHeader == nil {
				return executionInfo{}, fmt.Errorf("capella blinded block data missing")
			}
			h := vBlk.CapellaBlinded.Body.ExecutionPayloadHeader
			return executionInfo{BlockHash: h.BlockHash, ParentHash: h.ParentHash, BlockNumber: h.BlockNumber}, nil
		}
		if vBlk.Capella == nil || vBlk.Capella.Body == nil ||
			vBlk.Capella.Body.ExecutionPayload == nil {
			return executionInfo{}, fmt.Errorf("capella block data missing")
		}
		p := vBlk.Capella.Body.ExecutionPayload
		return executionInfo{BlockHash: p.BlockHash, ParentHash: p.ParentHash, BlockNumber: p.BlockNumber}, nil

	case spec.DataVersionDeneb:
		if vBlk.Blinded {
			if vBlk.DenebBlinded == nil || vBlk.DenebBlinded.Body == nil ||
				vBlk.DenebBlinded.Body.ExecutionPayloadHeader == nil {
				return executionInfo{}, fmt.Errorf("deneb blinded block data missing")
			}
			h := vBlk.DenebBlinded.Body.ExecutionPayloadHeader
			return executionInfo{BlockHash: h.BlockHash, ParentHash: h.ParentHash, BlockNumber: h.BlockNumber}, nil
		}
		if vBlk.Deneb == nil || vBlk.Deneb.Block == nil || vBlk.Deneb.Block.Body == nil ||
			vBlk.Deneb.Block.Body.ExecutionPayload == nil {
			return executionInfo{}, fmt.Errorf("deneb block data missing")
		}
		p := vBlk.Deneb.Block.Body.ExecutionPayload
		return executionInfo{BlockHash: p.BlockHash, ParentHash: p.ParentHash, BlockNumber: p.BlockNumber}, nil

	case spec.DataVersionElectra:
		if vBlk.Blinded {
			if vBlk.ElectraBlinded == nil || vBlk.ElectraBlinded.Body == nil ||
				vBlk.ElectraBlinded.Body.ExecutionPayloadHeader == nil {
				return executionInfo{}, fmt.Errorf("electra blinded block data missing")
			}
			h := vBlk.ElectraBlinded.Body.ExecutionPayloadHeader
			return executionInfo{BlockHash: h.BlockHash, ParentHash: h.ParentHash, BlockNumber: h.BlockNumber}, nil
		}
		if vBlk.Electra == nil || vBlk.Electra.Block == nil || vBlk.Electra.Block.Body == nil ||
			vBlk.Electra.Block.Body.ExecutionPayload == nil {
			return executionInfo{}, fmt.Errorf("electra block data missing")
		}
		p := vBlk.Electra.Block.Body.ExecutionPayload
		return executionInfo{BlockHash: p.BlockHash, ParentHash: p.ParentHash, BlockNumber: p.BlockNumber}, nil

	case spec.DataVersionFulu:
		if vBlk.Blinded {
			if vBlk.FuluBlinded == nil || vBlk.FuluBlinded.Body == nil ||
				vBlk.FuluBlinded.Body.ExecutionPayloadHeader == nil {
				return executionInfo{}, fmt.Errorf("fulu blinded block data missing")
			}
			h := vBlk.FuluBlinded.Body.ExecutionPayloadHeader
			return executionInfo{BlockHash: h.BlockHash, ParentHash: h.ParentHash, BlockNumber: h.BlockNumber}, nil
		}
		if vBlk.Fulu == nil || vBlk.Fulu.Block == nil || vBlk.Fulu.Block.Body == nil ||
			vBlk.Fulu.Block.Body.ExecutionPayload == nil {
			return executionInfo{}, fmt.Errorf("fulu block data missing")
		}
		p := vBlk.Fulu.Block.Body.ExecutionPayload
		return executionInfo{BlockHash: p.BlockHash, ParentHash: p.ParentHash, BlockNumber: p.BlockNumber}, nil

	default:
		return executionInfo{}, fmt.Errorf("unsupported block version %d", vBlk.Version)
	}
}

func proposalCommonFields(vBlk *api.VersionedProposal) ([]zap.Field, []attribute.KeyValue) {
	if vBlk == nil {
		err := fmt.Errorf("proposal is nil")
		return []zap.Field{zap.NamedError("proposal_err", err)}, []attribute.KeyValue{observability.BeaconBlockIsBlindedAttribute(false)}
	}

	logFields := []zap.Field{
		zap.String("version", vBlk.Version.String()),
		zap.Bool("blinded", vBlk.Blinded),
	}
	traceAttrs := []attribute.KeyValue{
		observability.BeaconBlockIsBlindedAttribute(vBlk.Blinded),
	}

	blockRoot, err := vBlk.Root()
	if err != nil {
		logFields = append(logFields, zap.NamedError("blockRoot_err", err))
	} else {
		logFields = append(logFields, fields.BlockRoot(blockRoot))
		traceAttrs = append(traceAttrs, observability.BeaconBlockRootAttribute(blockRoot))
	}

	parentRoot, err := vBlk.ParentRoot()
	if err != nil {
		logFields = append(logFields, zap.NamedError("parentRoot_err", err))
	} else {
		logFields = append(logFields, zap.String("parent_root", hex.EncodeToString(parentRoot[:])))
		traceAttrs = append(traceAttrs, observability.BeaconBlockParentRootAttribute(parentRoot))
	}

	execInfo, err := extractExecutionInfo(vBlk)
	if err != nil {
		logFields = append(logFields, zap.NamedError("execution_err", err))
	} else {
		logFields = append(
			logFields,
			fields.BlockHash(execInfo.BlockHash),
			zap.String("execution_parent_hash", hex.EncodeToString(execInfo.ParentHash[:])),
			zap.Uint64("execution_block_number", execInfo.BlockNumber),
		)
		traceAttrs = append(traceAttrs, observability.BeaconBlockHashAttribute(execInfo.BlockHash))
	}

	return logFields, traceAttrs
}
