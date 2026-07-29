package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

var _ Runner = (*ProposerPreferencesRunner)(nil)

// ProposerPreferencesRunner is the registered proposer-preferences runner for a validator (SIP #94
// §5). Unlike the single-duty runners (PTC, validator registration, ...), a validator can hold several
// upcoming proposal slots in the lookahead at once, and because preference messages route by
// MessageID (validator + role, with the proposal slot carried inside the message) they all arrive
// here. A single per-(validator, role) runner state can only hold one slot, so this dispatches each
// proposal slot to its own proposerPreferencesSlotRunner; otherwise concurrently-emitted slots would
// overwrite or reject one another.
//
// The embedded BaseRunner provides only the static Runner surface (role, share, persistence); the
// per-slot sub-runners own the actual duty state, so HasRunningDuty is overridden to aggregate them.
type ProposerPreferencesRunner struct {
	*BaseRunner

	opts ProposerPreferencesRunnerOptions

	// bySlot holds one sub-runner per concurrently-active proposal slot. Accessed only from the
	// validator's single message-processing goroutine.
	bySlot map[phase0.Slot]*proposerPreferencesSlotRunner

	// pending stashes every §5 partial-signature message by proposal slot so StartNewDuty can replay
	// it into a new (or replacement) sub-runner. Operators broadcast their §5 partial exactly once, at
	// their own emission tick, and those ticks skew across the committee (registration/event-sync
	// timing); a partial that arrives before the local duty (re)starts would otherwise be lost — the
	// sender does not re-broadcast, and message validation would drop a same-root re-broadcast as a
	// duplicate anyway. Bounded per slot (committee × distinct-root cap), pruned with evictPastSlots.
	// Accessed only from the validator's single message-processing goroutine.
	pending map[phase0.Slot][]*spectypes.PartialSignatureMessages
}

// maxPendingRootsPerSigner caps stashed partials per (slot, signer): the wire admits at most this
// many distinct §5-role signing roots — the preference budget plus the request-auth budget, the
// same shared constants message validation enforces.
const maxPendingRootsPerSigner = gloas.MaxProposerPreferencesDistinctRoots + gloas.MaxRequestAuthDistinctRoots

// ProposerPreferencesRunnerOptions bundles the dependencies required by NewProposerPreferencesRunner.
type ProposerPreferencesRunnerOptions struct {
	BaseRunnerOptions

	FeeRecipientProvider feeRecipientProvider
	GasLimit             uint64

	// Builders is the cluster's direct-builder list (issue #2962, validated at startup): for each
	// authenticatable entry the slot sub-runners additionally threshold-sign a RequestAuthV1 per
	// upcoming proposal slot. Empty (the default) disables the overlay entirely.
	Builders []gloas.BuilderEntry
	// RequestAuthCache receives each reconstructed SignedRequestAuthV1 for the §4 produce path.
	RequestAuthCache *ssv.RequestAuthCache
}

func NewProposerPreferencesRunner(opts ProposerPreferencesRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, fmt.Errorf("must have one share")
	}

	return &ProposerPreferencesRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleProposerPreferences,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
		},
		opts:    opts,
		bySlot:  map[phase0.Slot]*proposerPreferencesSlotRunner{},
		pending: map[phase0.Slot][]*spectypes.PartialSignatureMessages{},
	}, nil
}

func (r *ProposerPreferencesRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	r.evictPastSlots()

	// One sub-runner per proposal slot; a re-emission for the same slot (e.g. after a reorg, or an
	// indices-change re-emit) replaces the prior one so it freezes the current dependent_root. The
	// replaced incarnation is concluded so its outcome watcher doesn't report a false "stuck", and its
	// submitted preference carries over so an unchanged re-emission stays idempotent (no duplicate
	// broadcast or beacon-node submit; see executeDuty).
	slot := validatorDuty.DutySlot()
	sub := newProposerPreferencesSlotRunner(r.opts)
	if prev, ok := r.bySlot[slot]; ok {
		sub.submittedPreferences = prev.submittedPreferences
		sub.broadcastPreferences = prev.broadcastPreferences
		// The auth markers carry over too — roots are re-emission-invariant (see the field docs).
		sub.broadcastAuthRoots = prev.broadcastAuthRoots
		sub.reconstructedAuthRoots = prev.reconstructedAuthRoots
		if prev.hasDutyRunning() {
			prev.markDutyNotRequired() // superseded by the re-emission, not stuck
		}
	}
	r.bySlot[slot] = sub
	if err := sub.StartNewDuty(ctx, logger, duty, quorum); err != nil {
		return err
	}

	// Replay the stashed partials for this proposal slot. Peers broadcast their §5-role partials
	// once, at their own emission tick, so they may predate this (re)start; the stash is the only
	// recovery path — there is no re-broadcast, and message validation would dedup one by signing
	// root anyway. A stashed preference partial that doesn't match the freshly frozen preference
	// fails signature verification inside the sub-runner and is skipped; a stashed request-auth
	// partial outside the freshly frozen auth-root set is skipped the same way.
	//
	// The gate is duty-ASSIGNED, not running: a re-emission that concluded immediately (unchanged
	// preference → not-required sets State.Succeeded) still must replay auth partials into its
	// fresh container, or a yet-unreconstructed auth could never reach quorum again. Preference
	// partials replayed into a concluded duty bounce off the succeeded-gate harmlessly; the auth
	// rounds have no such gate by design.
	if sub.hasDutyAssigned() {
		for _, stashed := range r.pending[slot] {
			if err := sub.ProcessPreConsensus(ctx, logger, stashed); err != nil {
				logger.Debug("skipped stashed proposer-preferences partial on replay",
					fields.Slot(slot), zap.Error(err))
			}
		}
	}
	return nil
}

func (r *ProposerPreferencesRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	// Stash every §5-role partial — preference and request-auth alike (bounded, deduplicated) — even
	// when a sub-runner exists: a later re-emission replaces the sub-runner and its containers, and
	// peers won't re-broadcast, so the stash is what re-seeds the replacement (see StartNewDuty).
	r.stashPending(signedMsg)

	sub, ok := r.bySlot[signedMsg.Slot]
	if !ok {
		// No sub-runner for this proposal slot — it hasn't executed here yet (the stash above replays
		// once it starts), or it already concluded and was evicted. Retryable so a message racing the
		// duty start also lands via the queue replay.
		return NewRetryableError(spectypes.WrapError(spectypes.NoRunningDutyErrorCode, ErrNoDutyAssigned))
	}
	return sub.ProcessPreConsensus(ctx, logger, signedMsg)
}

// stashPending records a §5-role partial for its proposal slot so StartNewDuty can replay it.
// Duplicates by (signer, signing root) are skipped — roots are globally distinct across the two
// message types (different signing domains), so one keyspace serves both; a slot's stash is capped
// at the committee size times the wire's combined per-signer distinct-root budget, so a full stash
// can only mean noise.
func (r *ProposerPreferencesRunner) stashPending(signedMsg *spectypes.PartialSignatureMessages) {
	if signedMsg == nil || len(signedMsg.Messages) != 1 {
		return // §5-role partials (preference and request-auth alike) carry exactly one message
	}
	msg := signedMsg.Messages[0]
	stash := r.pending[signedMsg.Slot]
	for _, existing := range stash {
		e := existing.Messages[0]
		if e.Signer == msg.Signer && e.SigningRoot == msg.SigningRoot {
			return
		}
	}
	if len(stash) >= len(r.GetShare().Committee)*maxPendingRootsPerSigner {
		return
	}
	r.pending[signedMsg.Slot] = append(stash, signedMsg)
}

func (r *ProposerPreferencesRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return fmt.Errorf("no consensus phase for proposer preferences")
}

func (r *ProposerPreferencesRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return fmt.Errorf("no post-consensus phase for proposer preferences")
}

// HasRunningDuty reports whether any proposal slot is still running (the embedded BaseRunner's own
// state is unused — the sub-runners hold the duties).
func (r *ProposerPreferencesRunner) HasRunningDuty() bool {
	for _, sub := range r.bySlot {
		if sub.HasRunningDuty() {
			return true
		}
	}
	return false
}

// evictPastSlots drops sub-runners (and stashed partials) whose proposal slot has passed; the
// preference is moot once the proposal slot arrives, and convergence completes well before it.
func (r *ProposerPreferencesRunner) evictPastSlots() {
	current := r.NetworkConfig.EstimatedCurrentSlot()
	for slot := range r.bySlot {
		if slot < current {
			delete(r.bySlot, slot)
		}
	}
	for slot := range r.pending {
		if slot < current {
			delete(r.pending, slot)
		}
	}
}

func (r *ProposerPreferencesRunner) GetNetwork() protocolp2p.Network { return r.opts.Network }

func (r *ProposerPreferencesRunner) GetBeaconNode() beacon.BeaconNode { return r.opts.Beacon }

func (r *ProposerPreferencesRunner) GetSigner() ekm.BeaconSigner { return r.opts.Signer }

func (r *ProposerPreferencesRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.opts.OperatorSigner
}

// expectedPreConsensusRootsAndDomain / expectedPostConsensusRootsAndDomain / executeDuty are part of
// the Runner interface but run on the per-slot sub-runners, never the dispatcher.
func (r *ProposerPreferencesRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, fmt.Errorf("proposer preferences dispatcher has no frozen preference")
}

func (r *ProposerPreferencesRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, fmt.Errorf("no post-consensus roots for proposer preferences")
}

func (r *ProposerPreferencesRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	return fmt.Errorf("proposer preferences dispatcher does not execute duties directly")
}

// Only the static BaseRunner is persisted; the per-slot sub-runners are transient (re-emitted by the
// duty handler).
func (r *ProposerPreferencesRunner) MarshalJSON() ([]byte, error) {
	type proposerPreferencesRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}
	return json.Marshal(&proposerPreferencesRunnerJSON{BaseRunner: r.BaseRunner})
}

func (r *ProposerPreferencesRunner) UnmarshalJSON(data []byte) error {
	type proposerPreferencesRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}
	aux := &proposerPreferencesRunnerJSON{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
	}
	r.BaseRunner = aux.BaseRunner
	if r.bySlot == nil {
		r.bySlot = map[phase0.Slot]*proposerPreferencesSlotRunner{}
	}
	if r.pending == nil {
		r.pending = map[phase0.Slot][]*spectypes.PartialSignatureMessages{}
	}
	return nil
}

func (r *ProposerPreferencesRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *ProposerPreferencesRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

func (r *ProposerPreferencesRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode ProposerPreferencesRunner: %w", err)
	}
	return sha256.Sum256(marshaledRoot), nil
}

var _ Runner = (*proposerPreferencesSlotRunner)(nil)

// proposerPreferencesSlotRunner runs the Gloas (ePBS) proposer-preferences duty for one proposal slot
// (SIP #94 §5), driven by ProposerPreferencesRunner (one per concurrently-active slot). Like the PTC
// runner it has no consensus or pre-consensus negotiation — each operator builds the preference from
// its own beacon node and a per-validator signature reconstructs only once a threshold of operators
// converged on byte-identical preferences (honest convergence, not consensus).
//
// duty.Slot is the proposal slot the preference targets, which is also the slot carried on the
// partial-signature message and the slot whose epoch fixes the signing domain — keeping all three in
// step with the base runner's slot checks. The duty executes (emits) earlier, near the current slot,
// so the message rides a future slot; permitting that is the message-validation layer's job.
type proposerPreferencesSlotRunner struct {
	*BaseRunner

	beacon               beacon.BeaconNode
	network              protocolp2p.Network
	signer               ekm.BeaconSigner
	operatorSigner       ssvtypes.OperatorSigner
	feeRecipientProvider feeRecipientProvider
	gasLimit             uint64

	// proposerPreferences is the operator's frozen observation for this proposal slot: the preference
	// it built (including the dependent_root its own beacon node reported). Incoming partial signatures
	// are validated and aggregated against exactly this object's signing root; nil means the duty has
	// not executed yet.
	proposerPreferences *gloas.ProposerPreferences

	// submittedPreferences is the preference this proposal slot already submitted to the beacon node,
	// carried across sub-runner replacements by the dispatcher. executeDuty compares against it so a
	// re-emission that would rebuild the exact same preference (e.g. an indices-change re-emit under an
	// unchanged dependent_root) concludes as not-required instead of duplicating the gossip broadcast
	// (peers dedup it by signing root) and the beacon-node submit.
	submittedPreferences *gloas.ProposerPreferences

	// broadcastPreferences is the preference this proposal slot already broadcast a partial signature
	// for, carried across sub-runner replacements like submittedPreferences. It covers the in-flight
	// case (broadcast, quorum still converging): a re-emission that rebuilds it byte-identically keeps
	// converging without re-signing — peers would reject the identical re-broadcast as a same-peer
	// duplicate, penalizing this operator's gossip score for nothing (issue #2934); the dispatcher's
	// stash replay re-seeds the replacement instead, our own first partial included.
	broadcastPreferences *gloas.ProposerPreferences

	// builders is the cluster's direct-builder list (issue #2962 B1): for each authenticatable entry
	// executeDuty freezes and threshold-signs a RequestAuthV1{data, proposal_slot} alongside the §5
	// preference. Empty disables the request-auth round entirely.
	builders         []gloas.BuilderEntry
	requestAuthCache *ssv.RequestAuthCache

	// requestAuths maps each frozen RequestAuthV1's signing root to the object and its builders;
	// incoming RequestAuthPartialSig messages are admitted only against these roots. nil until the
	// duty executes here; empty when it executed but could freeze nothing (domain-fetch failure),
	// so peer partials hard-fail as unknown roots instead of burning queue retries — the
	// dispatcher stash keeps them for replay should a re-emission freeze successfully.
	requestAuths map[[32]byte]*frozenRequestAuth

	// requestAuthContainer collects request-auth partials separately from the preference round:
	// different signing domains cannot share the base pre-consensus round, and auth collection must
	// keep running after the preference concludes the duty.
	requestAuthContainer *ssv.PartialSigContainer

	// broadcastAuthRoots and reconstructedAuthRoots carry across sub-runner replacements like
	// broadcastPreferences: auth roots are re-emission-invariant, so a replacement must neither
	// re-broadcast a root already out (a same-peer duplicate, issue #2934) nor redo a
	// reconstruction its stash replay would re-reach quorum for.
	//
	// TODO(gloas): the markers are in-memory, so a restart mid-lookahead re-broadcasts every auth
	// root and peers REJECT the same-peer duplicates — up to entry-cap+1 penalized messages per
	// pending slot (§5 preference included), vs 1 pre-overlay. Gauge on a builders-configured
	// devnet before considering persistence.
	broadcastAuthRoots     map[[32]byte]struct{}
	reconstructedAuthRoots map[[32]byte]struct{}
}

func newProposerPreferencesSlotRunner(opts ProposerPreferencesRunnerOptions) *proposerPreferencesSlotRunner {
	return &proposerPreferencesSlotRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleProposerPreferences,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
		},

		beacon:                 opts.Beacon,
		network:                opts.Network,
		signer:                 opts.Signer,
		operatorSigner:         opts.OperatorSigner,
		feeRecipientProvider:   opts.FeeRecipientProvider,
		gasLimit:               opts.GasLimit,
		builders:               opts.Builders,
		requestAuthCache:       opts.RequestAuthCache,
		broadcastAuthRoots:     map[[32]byte]struct{}{},
		reconstructedAuthRoots: map[[32]byte]struct{}{},
	}
}

func (r *proposerPreferencesSlotRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	// Clear any prior observations; executeDuty re-freezes them, so a not-yet-executed duty stays nil.
	r.proposerPreferences = nil
	r.requestAuths = nil
	r.requestAuthContainer = ssv.NewPartialSigContainer(quorum)
	return r.baseStartNewNonBeaconDuty(ctx, logger, r, validatorDuty, quorum)
}

func (r *proposerPreferencesSlotRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	if signedMsg.Type == spectypes.RequestAuthPartialSig {
		return r.processRequestAuthPartial(ctx, logger, signedMsg)
	}

	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// A late message for a concluded slot is retryable (the sub-runner lingers until evicted).
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing proposer preferences message: %w", err)
	}

	// quorum returns true only once (the first time it is reached).
	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here; the quorum fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	if r.proposerPreferences == nil {
		return fmt.Errorf("reached quorum without frozen proposer preferences")
	}

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	fullSig, err := r.State.ReconstructBeaconSig(r.State.PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature is invalid, surface which partial signatures were at fault.
		r.FallBackAndVerifyEachSignature(r.State.PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err)
	}
	var signature phase0.BLSSignature
	copy(signature[:], fullSig)

	signed := &gloas.SignedProposerPreferences{
		Message:   r.proposerPreferences,
		Signature: signature,
	}
	if err := r.beacon.SubmitProposerPreferences(ctx, []*gloas.SignedProposerPreferences{signed}); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposerPreferences)
		const errMsg = "could not submit proposer preferences"
		logger.Error(errMsg, fields.Slot(r.proposerPreferences.ProposalSlot), zap.Error(err))
		return fmt.Errorf("%s: %w", errMsg, err)
	}

	recordSuccessfulSubmission(ctx, 1, r.NetworkConfig.EstimatedEpochAtSlot(r.proposerPreferences.ProposalSlot), spectypes.BNRoleProposerPreferences)
	r.submittedPreferences = r.proposerPreferences
	r.markDutySucceeded()
	logger.Info("✔️ successfully submitted proposer preferences", fields.Slot(r.proposerPreferences.ProposalSlot))
	return nil
}

func (r *proposerPreferencesSlotRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return fmt.Errorf("no consensus phase for proposer preferences")
}

func (r *proposerPreferencesSlotRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return fmt.Errorf("no post-consensus phase for proposer preferences")
}

func (r *proposerPreferencesSlotRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	if r.proposerPreferences == nil {
		return nil, spectypes.DomainError, fmt.Errorf("no frozen proposer preferences")
	}
	return []ssz.HashRoot{r.proposerPreferences}, phase0.DomainType(spectypes.DomainProposerPreferences), nil
}

func (r *proposerPreferencesSlotRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, fmt.Errorf("no post-consensus roots for proposer preferences")
}

func (r *proposerPreferencesSlotRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	proposalSlot := validatorDuty.DutySlot()

	// The request-auth round rides every execution of the §5 duty, ahead of the preference logic so
	// none of its early-return paths can skip it; it never fails the duty.
	r.runRequestAuthRound(ctx, logger, validatorDuty, proposalSlot)

	preferences, err := r.buildProposerPreferences(ctx, proposalSlot)
	if err != nil {
		// Building hits the beacon node (dependent-root fetch) and validator config (fee recipient);
		// a failure there is operational, so record a failed duty to surface it in metrics.
		logger.Warn("proposer preferences failed: could not build preferences", fields.Slot(proposalSlot), zap.Error(err))
		r.markDutyFailed(err)
		return nil
	}

	if r.submittedPreferences != nil && *preferences == *r.submittedPreferences {
		// A re-emission rebuilt the exact preference this slot already submitted: nothing changed, so
		// re-signing it would only produce a duplicate broadcast (dropped by peers' signing-root dedup)
		// and a duplicate beacon-node submit. Conclude quietly.
		logger.Debug("proposer preferences unchanged since last successful submit; skipping re-emission",
			fields.Slot(proposalSlot))
		r.markDutyNotRequired()
		return nil
	}

	// Freeze the observation: peers' partial signatures are validated and aggregated against exactly
	// this object's signing root, so only operators that converged on identical preferences (same
	// dependent_root, fee recipient, gas limit) reach quorum.
	r.proposerPreferences = preferences

	logger.Debug("built proposer preferences",
		fields.Slot(proposalSlot),
		zap.String("dependent_root", preferences.DependentRoot.String()),
		fields.FeeRecipient(preferences.FeeRecipient[:]),
		zap.Uint64("target_gas_limit", preferences.TargetGasLimit))

	if r.broadcastPreferences != nil && *preferences == *r.broadcastPreferences {
		// A prior incarnation of this slot already broadcast this exact preference (quorum still
		// converging): a re-broadcast would be rejected by peers as a same-peer duplicate and only
		// self-inflict a gossip-scoring penalty (issue #2934) — and it is useless anyway, since peers
		// stash the first copy. Keep the duty running so the stash replay (see StartNewDuty) and live
		// partials complete the quorum against the frozen preference above.
		logger.Debug("proposer preferences unchanged since last broadcast; skipping re-broadcast",
			fields.Slot(proposalSlot))
		return nil
	}

	msg, err := signBeaconObject(ctx, r, r.NetworkConfig, validatorDuty, preferences, proposalSlot, phase0.DomainType(spectypes.DomainProposerPreferences))
	if err != nil {
		return fmt.Errorf("could not sign proposer preferences: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ProposerPreferencesPartialSig,
		Slot:     proposalSlot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey[:], msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast proposer preferences partial sig: %w", err)
	}
	r.broadcastPreferences = preferences
	return nil
}

// buildProposerPreferences assembles the preference for the proposal slot from this operator's own
// view: fee recipient and target gas limit from validator config (matching validator registration),
// and the dependent_root of the proposer duties for the proposal slot's epoch — the seed that fixed
// this proposal assignment (SIP #94 §5), fetched per-operator so convergence is over identical roots.
func (r *proposerPreferencesSlotRunner) buildProposerPreferences(ctx context.Context, proposalSlot phase0.Slot) (*gloas.ProposerPreferences, error) {
	validatorPubKey := r.GetShare().ValidatorPubKey

	feeRecipient, err := r.feeRecipientProvider.GetFeeRecipient(validatorPubKey)
	if err != nil {
		return nil, fmt.Errorf("could not get fee recipient for validator %x: %w", validatorPubKey, err)
	}

	gasLimit := r.gasLimit
	if gasLimit == 0 {
		gasLimit = DefaultGasLimit
	}

	epoch := r.NetworkConfig.EstimatedEpochAtSlot(proposalSlot)
	dependentRoot, err := r.beacon.ProposerDutiesDependentRoot(ctx, epoch)
	if err != nil {
		return nil, fmt.Errorf("could not fetch proposer-duties dependent root for epoch %d: %w", epoch, err)
	}

	// KNOWN ISSUE (SIP-94 §5 publish-finality — pending): dependent_root/fee_recipient/target_gas_limit are
	// read here at emit time and the preference is published once pre-consensus quorum is reached, with no
	// guard holding publication until they are final. A reorg that changes dependent_root is handled — the
	// scheduler re-emits only on a real change and message validation admits the new signing root — but a
	// preference already published under a soon-to-change root is not retracted. Low severity (reorg-gated,
	// §5 is observational); add a finality hold only if it bites on devnet.
	return &gloas.ProposerPreferences{
		DependentRoot:  dependentRoot,
		ProposalSlot:   proposalSlot,
		ValidatorIndex: r.GetShare().ValidatorIndex,
		FeeRecipient:   feeRecipient,
		TargetGasLimit: gasLimit,
	}, nil
}

func (r *proposerPreferencesSlotRunner) GetNetwork() protocolp2p.Network { return r.network }

func (r *proposerPreferencesSlotRunner) GetBeaconNode() beacon.BeaconNode { return r.beacon }

func (r *proposerPreferencesSlotRunner) GetSigner() ekm.BeaconSigner { return r.signer }

func (r *proposerPreferencesSlotRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Only BaseRunner is persisted; the frozen observation is transient per-duty state.
func (r *proposerPreferencesSlotRunner) MarshalJSON() ([]byte, error) {
	type proposerPreferencesSlotRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}
	return json.Marshal(&proposerPreferencesSlotRunnerJSON{BaseRunner: r.BaseRunner})
}

func (r *proposerPreferencesSlotRunner) UnmarshalJSON(data []byte) error {
	type proposerPreferencesSlotRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
	}
	aux := &proposerPreferencesSlotRunnerJSON{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
	}
	r.BaseRunner = aux.BaseRunner
	return nil
}

func (r *proposerPreferencesSlotRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *proposerPreferencesSlotRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

func (r *proposerPreferencesSlotRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode proposerPreferencesSlotRunner: %w", err)
	}
	return sha256.Sum256(marshaledRoot), nil
}
