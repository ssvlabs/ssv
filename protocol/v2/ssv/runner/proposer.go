package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

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

	// builders is the cluster's direct-builder config, resolved once at construction (issue #2962, phase 2):
	// the produceBlockV4 POST body is assembled from it plus the per-slot reconstructed auths. Not
	// Configured() -> a neutral local-build POST body.
	builders gloas.ResolvedBuilderConfig
	// requestAuthCache holds the per-slot reconstructed builder auths this operator attaches to the
	// produceBlockV4 POST. Shared with the §5 dispatcher that writes it; nil pre-Gloas / no overlay.
	requestAuthCache *ssv.RequestAuthCache
	// gloasProducedRoot / gloasBuilderURL / gloasProducedEnvelope record this operator's own §4 produce
	// output for the slot: the produced block root, any winning builder URL, and a self-build's reveal data.
	// At publish, Eth-Builder-Url is echoed only when the decided block matches gloasProducedRoot
	// (owner-match — see decidedBuilderURL), and the reveal is published only when the produced envelope is
	// the one the decided value commits to — this operator is then the builder operator (see publishEnvelope).
	gloasProducedRoot     [32]byte
	gloasBuilderURL       string
	gloasProducedEnvelope *gloas.ProducedEnvelope
	// gloasEnvelopeSigningRoot is the decided value's §6 envelope signing root, recorded at the duty's first
	// post-consensus quorum at a Gloas slot and zero when no envelope root is expected (pre-Gloas, external
	// bid). awaitingEnvelope reads it: it runs on the queue consumer's path, without a context for the beacon
	// domain lookup that deriving the root from the decided value needs, and the root is fixed once decided.
	gloasEnvelopeSigningRoot [32]byte
}

var _ PostConsensusAwaiter = (*ProposerRunner)(nil)

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

	// Builders / RequestAuthCache feed the phase-2 produceBlockV4 POST body (issue #2962). Optional
	// (empty / nil pre-Gloas or when the direct-builder overlay is unconfigured).
	Builders         gloas.BuilderConfig
	RequestAuthCache *ssv.RequestAuthCache
}

func NewProposerRunner(opts ProposerRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}

	// Resolve the builder config once per validator — the §4 produce path reads the pre-decoded form.
	// Startup already validated it.
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

		proposerDelay:     opts.ProposerDelay,
		proposerDelayEPBS: opts.ProposerDelayEPBS,
		builders:          builders,
		requestAuthCache:  opts.RequestAuthCache,
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

// gloasProposalInput fetches the Gloas (ePBS) block this operator would propose and wraps it as the QBFT
// consensus value: GloasProposalData, the block plus the self-build payload_root (SIP #94 §4). The block
// carries only the execution-payload bid, so unlike the pre-Gloas path there is no blinding; the
// payload_root — hash_tree_root(envelope.payload), the one envelope field the block does not commit to —
// lets every operator derive and sign the §6 envelope in the block's post-consensus round. Zero for an
// external bid.
func (r *ProposerRunner) gloasProposalInput(ctx context.Context, logger *zap.Logger, duty *spectypes.ValidatorDuty, randaoReveal []byte) (*spectypes.ProposerConsensusData, error) {
	start := time.Now()
	builderConfig := r.gloasBuilderConfig(ctx, duty.Slot)
	produced, err := r.GetBeaconNode().GetGloasBeaconBlock(ctx, duty.Slot, r.graffiti, randaoReveal, builderConfig)
	if err != nil {
		return nil, fmt.Errorf("get gloas beacon block: %w", err)
	}
	if produced == nil || produced.Block == nil {
		return nil, fmt.Errorf("get gloas beacon block: empty produce result")
	}
	block := produced.Block
	if block.Body == nil || block.Body.SignedExecutionPayloadBid == nil || block.Body.SignedExecutionPayloadBid.Message == nil {
		return nil, fmt.Errorf("get gloas beacon block: produced block carries no execution payload bid")
	}
	proposalData := &gloas.GloasProposalData{Block: block}

	// Remember this operator's own produce output so the §4 publish echoes Eth-Builder-Url only when the
	// decided block is this operator's own (owner-match — see decidedBuilderURL). The root is also what we
	// would sign, so a block we can't hash is unusable.
	root, err := block.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("hash tree root of produced gloas block: %w", err)
	}
	r.gloasProducedRoot, r.gloasBuilderURL, r.gloasProducedEnvelope = root, produced.BuilderURL, nil

	if proposalData.SelfBuild() {
		if produced.Envelope == nil || produced.Envelope.Envelope == nil || produced.Envelope.Envelope.Payload == nil {
			// The beacon node self-built but did not honor include_payload=true. Without the payload there is
			// no payload_root to decide on, and the value check rejects a self-build value without one — so
			// the block is unproposable, and the cluster could not reveal it anyway (SIP #94 §4/§6).
			return nil, fmt.Errorf("beacon node returned a self-build block without its payload despite include_payload=true")
		}
		payloadRoot, err := produced.Envelope.Envelope.Payload.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("hash tree root of produced execution payload: %w", err)
		}
		proposalData.PayloadRoot = payloadRoot
		// The reveal data belongs to the builder operator alone: it is published only if this block is
		// decided (see publishEnvelope).
		r.gloasProducedEnvelope = produced.Envelope
	}

	byts, err := proposalData.Encode()
	if err != nil {
		return nil, fmt.Errorf("could not marshal gloas proposal data: %w", err)
	}

	logFields := []zap.Field{
		fields.Slot(duty.Slot),
		zap.Duration("proposer_delay", r.proposerDelayForSlot(duty.Slot)),
		fields.Took(time.Since(start)),
		fields.FeeRecipient(block.Body.SignedExecutionPayloadBid.Message.FeeRecipient[:]),
		zap.Bool("self_build", proposalData.SelfBuild()),
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
// the per-slot reconstructed auths (beacon-APIs#630), or nil when nothing is configured (the goclient then
// POSTs a neutral local-build config). Builders whose auth missed quorum this slot are omitted and counted
// for the E1 auth-unavailable signal; the top-level p2p knobs are always carried. The goclient falls back
// to GET per beacon node that predates #630.
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

	var (
		blkRootToSign spectypes.HashRoot
		// proposalData is the decided Gloas value; its self-build payload_root adds the §6 envelope entry.
		proposalData *gloas.GloasProposalData
	)
	if r.NetworkConfig.IsGloasAtSlot(cd.Duty.Slot) {
		// Gloas blocks have no spectypes block version; decode the §4 wrapper, whose block is the value to sign.
		var decErr error
		proposalData, decErr = gloas.DecodeGloasProposalData(cd.DataSSZ)
		if decErr != nil {
			return fmt.Errorf("could not decode gloas proposal data from consensus data: %w", decErr)
		}
		blkRootToSign = proposalData.Block
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
	entries := []*spectypes.PartialSignatureMessage{msg}

	if proposalData != nil && proposalData.SelfBuild() {
		// The §6 blinded envelope derives from the decided value alone, so every operator signs its root
		// under DomainBeaconBuilder as a second entry of the block's packet — the reveal needs no round of
		// its own (SIP #94 §4/§6).
		envelope, err := proposalData.DeriveBlindedEnvelope()
		if err != nil {
			return fmt.Errorf("could not derive blinded envelope: %w", err)
		}
		envelopeMsg, err := signBeaconObject(
			ctx,
			r,
			r.NetworkConfig,
			duty,
			envelope,
			cd.Duty.Slot,
			phase0.DomainType(spectypes.DomainBeaconBuilder),
		)
		if err != nil {
			return fmt.Errorf("failed signing blinded envelope: %w", err)
		}
		entries = append(entries, envelopeMsg)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     cd.Duty.Slot,
		Messages: entries,
	}

	r.measurements.StartPostConsensus()
	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.signAndBroadcastPostConsensusMsg(r.GetNetwork(), r.operatorSigner, r.GetShare().ValidatorPubKey, postConsensusMsg); err != nil {
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
	if err != nil {
		return fmt.Errorf("failed processing post consensus message: %w", err)
	}
	if !hasQuorum {
		return nil
	}

	validatorConsensusData := &spectypes.ProposerConsensusData{}
	if err := validatorConsensusData.Decode(r.State.DecidedValue); err != nil {
		return fmt.Errorf("could not decode decided validator consensus data: %w", err)
	}
	if r.NetworkConfig.IsGloasAtSlot(validatorConsensusData.Duty.Slot) {
		// At a Gloas slot the packet's two roots reach quorum independently, so the duty's outcome is
		// settled per root there.
		return r.processGloasPostConsensusQuorum(ctx, logger, span, validatorConsensusData, roots)
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

	// only 1 root, verified by expectedPostConsensusRootsAndDomains
	specSig, err := r.reconstructPostConsensusSig(roots[0])
	if err != nil {
		return err
	}

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

// reconstructPostConsensusSig reconstructs the validator's signature over root from the post-consensus
// quorum. If the reconstructed signature does not verify, each partial signature is verified in turn to
// surface the faulty signer.
func (r *ProposerRunner) reconstructPostConsensusSig(root [32]byte) (phase0.BLSSignature, error) {
	sig, err := r.State.ReconstructBeaconSig(r.State.PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		r.FallBackAndVerifyEachSignature(r.State.PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return phase0.BLSSignature{}, fmt.Errorf("got post-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], sig)
	return specSig, nil
}

// processGloasPostConsensusQuorum handles the roots that just reached post-consensus quorum at a Gloas
// slot. The packet carries the block root and, on the self-build path, the §6 blinded-envelope root; each
// reconstructs independently, in the same packet or a later one (SIP #94 §4/§6). The block root's quorum
// submits the block and finishes the duty; the envelope root's quorum publishes the reveal. The block goes
// first when both arrive together — the beacon node needs it before it accepts its envelope — and an
// envelope quorum is still acted on when the block submit failed: other operators submit the block too.
func (r *ProposerRunner) processGloasPostConsensusQuorum(ctx context.Context, logger *zap.Logger, span trace.Span, cd *spectypes.ProposerConsensusData, roots [][32]byte) error {
	blockSigningRoot, envelopeSigningRoot, err := r.gloasPostConsensusSigningRoots(ctx)
	if err != nil {
		if !r.hasDutySucceeded() {
			// A quorum fires only once, so a block quorum lost here is terminal for the duty.
			r.markDutyFailed(err)
		}
		return err
	}
	// Recorded before the block's quorum can finish the duty, so awaitingEnvelope holds from then on.
	r.gloasEnvelopeSigningRoot = envelopeSigningRoot

	var blockErr error
	if slices.Contains(roots, blockSigningRoot) {
		blockErr = r.submitGloasBlock(ctx, logger, span, cd, blockSigningRoot)
	}
	if envelopeSigningRoot != [32]byte{} && slices.Contains(roots, envelopeSigningRoot) {
		if err := r.publishEnvelope(ctx, logger, cd, envelopeSigningRoot); err != nil {
			return errors.Join(blockErr, err)
		}
	}
	return blockErr
}

// gloasPostConsensusSigningRoots resolves the decided value's expected block and §6 envelope signing roots,
// so the roots that reached quorum can be told apart. The envelope root is zero when the value expects none
// (external bid).
func (r *ProposerRunner) gloasPostConsensusSigningRoots(ctx context.Context) (block, envelope [32]byte, err error) {
	expected, err := r.expectedPostConsensusRootsAndDomains(ctx)
	if err != nil {
		return block, envelope, err
	}
	resolved, err := r.resolvePostConsensusSigningRoots(ctx, r, expected)
	if err != nil {
		return block, envelope, err
	}
	for _, e := range resolved {
		switch e.Domain {
		case spectypes.DomainProposer:
			block = e.SigningRoot
		case spectypes.DomainBeaconBuilder:
			envelope = e.SigningRoot
		}
	}
	return block, envelope, nil
}

// submitGloasBlock reconstructs the block signature from the quorum over signingRoot and publishes the
// decided Gloas (ePBS) block, finishing the duty. Every operator submits it — the ePBS block is bid-only so
// all hold it, keeping the pre-Gloas all-submit redundancy. That relies on the BN deduping duplicate
// submissions by root (battle-tested pre-Gloas; still to be confirmed against a real Gloas BN). The quorum
// fires only once, so a terminal failure here fails the duty and is not retried.
func (r *ProposerRunner) submitGloasBlock(ctx context.Context, logger *zap.Logger, span trace.Span, cd *spectypes.ProposerConsensusData, signingRoot [32]byte) (err error) {
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleProposer)

	sig, err := r.reconstructPostConsensusSig(signingRoot)
	if err != nil {
		return err
	}
	r.doppelgangerHandler.ReportQuorum(r.GetShare().ValidatorIndex)

	const submittingBlockProposalEvent = "submitting block proposal"
	span.AddEvent(submittingBlockProposalEvent)
	logger.Info(submittingBlockProposalEvent)

	proposalData, err := gloas.DecodeGloasProposalData(cd.DataSSZ)
	if err != nil {
		return fmt.Errorf("could not decode decided gloas proposal data: %w", err)
	}
	block := proposalData.Block
	logger.Debug("decided gloas block build source",
		fields.Slot(cd.Duty.Slot),
		zap.Bool("self_build", proposalData.SelfBuild()))

	start := time.Now()
	signedBlock := &gloas.SignedBeaconBlock{Message: block, Signature: sig}
	if err := r.GetBeaconNode().SubmitGloasBeaconBlock(ctx, signedBlock, r.decidedBuilderURL(block)); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposer)
		const errMsg = "could not submit gloas beacon block"
		logger.Error(errMsg, fields.Slot(cd.Duty.Slot), zap.Error(err))
		return fmt.Errorf("%s: %w", errMsg, err)
	}
	recordProposalBuildSource(ctx, gloasBuildSource(proposalData))
	return r.finishSubmittedProposal(ctx, logger, span, start, nil)
}

// publishEnvelope reconstructs the §6 envelope signature from the quorum over signingRoot and publishes
// the reveal — but only on the builder operator, the one whose own produceBlockV4 response holds the
// decided block, so that its produced envelope is the one the decided value commits to. Every other
// operator reconstructs and publishes nothing (SIP #94 §6). The duty's outcome is the block's, already
// recorded; a failure here is logged and counted (recordEnvelopePublish) but does not re-conclude the duty.
func (r *ProposerRunner) publishEnvelope(ctx context.Context, logger *zap.Logger, cd *spectypes.ProposerConsensusData, signingRoot [32]byte) error {
	sig, err := r.reconstructPostConsensusSig(signingRoot)
	if err != nil {
		return err
	}

	built, err := r.builtDecidedEnvelope(cd)
	if err != nil {
		return err
	}
	recordEnvelopeBuildMatch(ctx, built)
	if !built {
		logger.Debug("envelope signature reconstructed; this operator did not build the envelope, not publishing", fields.Slot(cd.Duty.Slot))
		return nil
	}

	if err := r.GetBeaconNode().SubmitExecutionPayloadEnvelope(ctx, r.gloasProducedEnvelope.Signed(sig)); err != nil {
		recordEnvelopePublish(ctx, false)
		const errMsg = "could not submit execution payload envelope"
		logger.Error(errMsg, fields.Slot(cd.Duty.Slot), zap.Error(err))
		return fmt.Errorf("%s: %w", errMsg, err)
	}
	recordEnvelopePublish(ctx, true)
	logger.Info("✅ published execution payload envelope", fields.Slot(cd.Duty.Slot))
	return nil
}

// builtDecidedEnvelope reports whether this operator's own produced envelope is the one the decided value
// commits to — its blinded form hashes to the envelope derived from the decided value — which makes this
// operator the builder operator, the only holder of the payload behind the reconstructed signature.
func (r *ProposerRunner) builtDecidedEnvelope(cd *spectypes.ProposerConsensusData) (bool, error) {
	if r.gloasProducedEnvelope == nil {
		return false, nil
	}
	proposalData, err := gloas.DecodeGloasProposalData(cd.DataSSZ)
	if err != nil {
		return false, fmt.Errorf("could not decode decided gloas proposal data: %w", err)
	}
	derived, err := proposalData.DeriveBlindedEnvelope()
	if err != nil {
		return false, fmt.Errorf("could not derive blinded envelope: %w", err)
	}
	derivedRoot, err := derived.HashTreeRoot()
	if err != nil {
		return false, fmt.Errorf("hash tree root of derived blinded envelope: %w", err)
	}
	produced, err := gloas.Blinded(r.gloasProducedEnvelope.Envelope)
	if err != nil {
		return false, fmt.Errorf("blind produced execution payload envelope: %w", err)
	}
	producedRoot, err := produced.HashTreeRoot()
	if err != nil {
		return false, fmt.Errorf("hash tree root of produced blinded envelope: %w", err)
	}
	return producedRoot == derivedRoot, nil
}

// awaitingEnvelope reports whether the block reached quorum — so the duty is finished — while the decided
// value's §6 envelope root has not: the window in which the proposer keeps accepting post-consensus
// packets, so an envelope quorum completing after the block's still publishes the reveal (SIP #94 §4).
// The window closes with the duty's slot, a reveal after it being useless. False pre-Gloas, on an external
// bid (no envelope root), and once the envelope reconstructs.
func (r *ProposerRunner) awaitingEnvelope() bool {
	if !r.hasDutySucceeded() || r.gloasEnvelopeSigningRoot == [32]byte{} {
		return false
	}
	slot, err := r.currentDutySlot()
	if err != nil || r.NetworkConfig.EstimatedCurrentSlot() > slot {
		return false
	}
	hasQuorum, _ := r.State.PostConsensusContainer.HasQuorum(r.GetShare().ValidatorIndex, r.gloasEnvelopeSigningRoot)
	return !hasQuorum
}

// AwaitingPostConsensus implements PostConsensusAwaiter: the finished duty's slot while awaitingEnvelope
// holds, so the queue consumer keeps delivering that slot's post-consensus packets.
func (r *ProposerRunner) AwaitingPostConsensus() (phase0.Slot, bool) {
	if !r.awaitingEnvelope() {
		return 0, false
	}
	slot, err := r.currentDutySlot()
	return slot, err == nil
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

// gloasBuildSource classifies a decided Gloas value for the build-source telemetry (issue #2962 E1).
func gloasBuildSource(proposalData *gloas.GloasProposalData) proposalBuildSource {
	if proposalData.SelfBuild() {
		return buildSourceLocal
	}
	return buildSourceBuilder
}

func (r *ProposerRunner) expectedPreConsensusRootsAndDomain() ([]spectypes.HashRoot, phase0.DomainType, error) {
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("current duty slot: %w", err)
	}
	epoch := r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot)
	return []spectypes.HashRoot{spectypes.SSZUint64(epoch)}, spectypes.DomainRandao, nil
}

// expectedPostConsensusRootsAndDomains an INTERNAL function, returns the expected post-consensus roots to
// sign, each with its domain: the block root under DomainProposer and, at a Gloas self-build slot, the §6
// blinded-envelope root under DomainBeaconBuilder — optional, since a peer may sign the block alone (SIP
// #94 §4/§6).
func (r *ProposerRunner) expectedPostConsensusRootsAndDomains(context.Context) ([]PostConsensusRoot, error) {
	validatorConsensusData := &spectypes.ProposerConsensusData{}
	err := validatorConsensusData.Decode(r.State.DecidedValue)
	if err != nil {
		return nil, fmt.Errorf("could not decode consensus data: %w", err)
	}

	if r.NetworkConfig.IsGloasAtSlot(validatorConsensusData.Duty.Slot) {
		proposalData, err := gloas.DecodeGloasProposalData(validatorConsensusData.DataSSZ)
		if err != nil {
			return nil, fmt.Errorf("could not decode gloas proposal data: %w", err)
		}
		roots := []PostConsensusRoot{{Root: proposalData.Block, Domain: spectypes.DomainProposer}}
		if proposalData.SelfBuild() {
			envelope, err := proposalData.DeriveBlindedEnvelope()
			if err != nil {
				return nil, fmt.Errorf("could not derive blinded envelope: %w", err)
			}
			roots = append(roots, PostConsensusRoot{Root: envelope, Domain: phase0.DomainType(spectypes.DomainBeaconBuilder), Optional: true})
		}
		return roots, nil
	}

	_, root, err := validatorConsensusData.GetBlockData()
	if err != nil {
		return nil, fmt.Errorf("could not get block data: %w", err)
	}
	return singleDomainPostConsensusRoots(spectypes.DomainProposer, root), nil
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

	// reset the cached original block, and this operator's own Gloas produce markers, at the beginning of a
	// new duty — a stale owner-match must not echo a previous slot's Eth-Builder-Url, and a stale envelope
	// root must not hold the new duty open (awaitingEnvelope)
	r.cachedFullBlock = nil
	r.cachedBlindedBlockSSZ = nil
	r.gloasProducedRoot, r.gloasBuilderURL, r.gloasProducedEnvelope = [32]byte{}, "", nil
	r.gloasEnvelopeSigningRoot = [32]byte{}

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
	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey, msgs); err != nil {
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
