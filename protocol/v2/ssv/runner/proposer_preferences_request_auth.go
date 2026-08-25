package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// The §5 dispatcher's request-auth rounds (issue #2962 B1): threshold-signing one BuilderRequestAuth
// per configured builder, riding the proposer-preferences duty. The per-slot auth state lives on
// proposerPreferencesSlotRunner (proposer_preferences.go); this file holds the round logic.

// frozenRequestAuth pairs a frozen BuilderRequestAuth with every configured builder relationship it
// authenticates: the signing root derives from (data, slot) alone — not the URL — so distinct
// entries sharing one pre-agreed token converge on one root, one broadcast, and one reconstruction
// serving them all.
type frozenRequestAuth struct {
	auth     *gloas.BuilderRequestAuth
	builders []frozenBuilderRef
}

// frozenBuilderRef names one configured builder relationship covered by a frozen auth.
type frozenBuilderRef struct {
	identity            string // gloas.BuilderIdentity — the RequestAuthCache key
	url                 string // the builder URL, for logging and the phase-3 preferences submit
	maxExecutionPayment uint64 // the configured cap, forwarded via submitBuilderPreferences (phase 3)
}

// runRequestAuthRound freezes one BuilderRequestAuth{data, proposal_slot} per configured builder,
// records its signing root so incoming partials can be admitted, and broadcasts this operator's
// partial — once per root, across re-emissions. Per-builder failures are logged and skipped, never
// failing the §5 duty: a builder whose auth misses quorum is simply not contactable for the slot,
// and the enshrined flow (gossip bids, self-build) stays available.
func (r *proposerPreferencesSlotRunner) runRequestAuthRound(ctx context.Context, logger *zap.Logger, validatorDuty *spectypes.ValidatorDuty, proposalSlot phase0.Slot) {
	if len(r.builders) == 0 {
		return
	}

	// DomainBuilderRequestAuth is genesis-style (computed locally by the beacon adapter, no BN
	// call); the epoch argument is ignored for it.
	domain, err := r.beacon.DomainData(ctx, r.NetworkConfig.EstimatedEpochAtSlot(proposalSlot), phase0.DomainType(spectypes.DomainBuilderRequestAuth))
	if err != nil {
		// Freeze an empty root set: executed-but-froze-nothing (see the requestAuths field doc).
		r.requestAuths = map[[32]byte]*frozenRequestAuth{}
		logger.Warn("request auth skipped: could not get domain data", fields.Slot(proposalSlot), zap.Error(err))
		return
	}

	r.requestAuths = make(map[[32]byte]*frozenRequestAuth, len(r.builders))
	for i := range r.builders {
		entry := &r.builders[i]
		data, err := entry.AuthDataBytes()
		if err != nil {
			// Unreachable when startup validation ran, but never swallow a real error silently.
			logger.Warn("request auth skipped: invalid auth data",
				fields.Slot(proposalSlot), zap.String("builder_url", entry.URL), zap.Error(err))
			continue
		}
		auth := &gloas.BuilderRequestAuth{Data: data, Slot: proposalSlot}
		root, err := spectypes.ComputeETHSigningRoot(auth, domain)
		if err != nil {
			logger.Warn("request auth skipped: could not compute signing root",
				fields.Slot(proposalSlot), zap.String("builder_url", entry.URL), zap.Error(err))
			continue
		}
		ref := frozenBuilderRef{identity: gloas.BuilderIdentity(entry.URL, data), url: entry.URL, maxExecutionPayment: entry.MaxExecutionPayment}
		if frozen, ok := r.requestAuths[root]; ok {
			// Another entry froze these exact bytes; register the extra relationship on the shared root.
			frozen.builders = append(frozen.builders, ref)
			continue
		}
		r.requestAuths[root] = &frozenRequestAuth{auth: auth, builders: []frozenBuilderRef{ref}}

		if _, done := r.broadcastAuthRoots[root]; done {
			continue // broadcast by a prior incarnation; stash replay and live partials finish its quorum
		}
		msg, err := signAsValidator(ctx, r, validatorDuty.ValidatorIndex, auth, proposalSlot, phase0.DomainType(spectypes.DomainBuilderRequestAuth), domain)
		if err != nil {
			logger.Warn("request auth skipped: could not sign",
				fields.Slot(proposalSlot), zap.String("builder_url", entry.URL), zap.Error(err))
			continue
		}
		msgs := &spectypes.PartialSignatureMessages{
			Type:     spectypes.RequestAuthPartialSig,
			Slot:     proposalSlot,
			Messages: []*spectypes.PartialSignatureMessage{msg},
		}
		if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey[:], msgs); err != nil {
			logger.Warn("request auth skipped: could not broadcast partial",
				fields.Slot(proposalSlot), zap.String("builder_url", entry.URL), zap.Error(err))
			continue
		}
		r.broadcastAuthRoots[root] = struct{}{}
	}
}

// processRequestAuthPartial collects request-auth partials into their own container and, on the
// first quorum for a root, reconstructs the builder-facing SignedBuilderRequestAuth into the shared
// cache. No succeeded-gate: the preference submission concluding the duty must not stop auth
// collection, which legitimately runs until the sub-runner is evicted.
func (r *proposerPreferencesSlotRunner) processRequestAuthPartial(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	if !r.hasDutyAssigned() {
		return NewRetryableError(spectypes.WrapError(spectypes.NoRunningDutyErrorCode, ErrNoDutyAssigned))
	}
	if err := r.validatePartialSigMsg(signedMsg, r.State.CurrentDuty.DutySlot()); err != nil {
		return fmt.Errorf("invalid request-auth partial: %w", err)
	}
	// The auth root, unlike the §5 preference, doesn't bind the validator index — tie the message
	// to this runner's share explicitly, as the post-consensus paths do.
	if err := r.validateValidatorIndexInPartialSigMsg(signedMsg); err != nil {
		return err
	}
	if len(signedMsg.Messages) != 1 {
		return errors.New("request-auth partial must carry exactly one message")
	}
	msg := signedMsg.Messages[0]

	if r.requestAuths == nil {
		if len(r.builders) == 0 {
			// No overlay here (never configured, or disabled for a remote signer): no root will
			// ever be frozen for this slot, so retrying cannot help.
			return errors.New("no builders configured")
		}
		// Duty assigned but not executed here yet: retryable, so a partial racing the duty start
		// also lands via the queue replay and the dispatcher stash.
		return NewRetryableError(spectypes.WrapError(spectypes.NoRunningDutyErrorCode, errors.New("no frozen request auths")))
	}
	frozen, ok := r.requestAuths[msg.SigningRoot]
	if !ok {
		// The sender's builder list or auth-data bytes diverge from ours; whatever quorum this root
		// can reach forms on the operators that share its config.
		return fmt.Errorf("unknown request-auth signing root %x", msg.SigningRoot)
	}
	if _, done := r.reconstructedAuthRoots[msg.SigningRoot]; done {
		return nil // reconstructed and cached, possibly by a prior incarnation; late partials add nothing
	}

	// quorum returns true only once per root (the first time it is reached).
	hasQuorum, _ := r.basePartialSigMsgProcessing(signedMsg, r.requestAuthContainer)
	if !hasQuorum {
		return nil
	}

	fullSig, err := r.State.ReconstructBeaconSig(r.requestAuthContainer, msg.SigningRoot, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature is invalid, surface which partial signatures were at fault.
		r.FallBackAndVerifyEachSignature(r.requestAuthContainer, msg.SigningRoot, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got request-auth quorum but it has invalid signatures: %w", err)
	}
	var signature phase0.BLSSignature
	copy(signature[:], fullSig)

	r.reconstructedAuthRoots[msg.SigningRoot] = struct{}{}
	signed := &gloas.SignedBuilderRequestAuth{Message: frozen.auth, Signature: signature}
	urls := make([]string, 0, len(frozen.builders))
	for _, ref := range frozen.builders {
		urls = append(urls, ref.url)
	}
	if r.requestAuthCache != nil {
		for _, ref := range frozen.builders {
			r.requestAuthCache.Store(frozen.auth.Slot, ref.identity, signed)
		}
	}
	r.submitBuilderPreferences(ctx, logger, signed, frozen.builders)
	recordRequestAuthReconstruction(ctx)
	logger.Info("✔️ reconstructed builder request auth",
		fields.Slot(frozen.auth.Slot), zap.Strings("builder_urls", urls))
	return nil
}

// submitBuilderPreferences forwards the reconstructed auth as the ahead-of-time per-builder preference
// (issue #2962 phase 3, beacon-APIs#630): one BuilderPreferencesEntry per builder sharing the auth, each
// carrying the proposer pubkey, the builder URL, and the configured max-execution-payment cap. Every
// operator submits via its own beacon node — the builder dedupes per proposer per slot. Best-effort: a
// failure never disturbs the §5/auth flow, only its metric and log.
func (r *proposerPreferencesSlotRunner) submitBuilderPreferences(ctx context.Context, logger *zap.Logger, signed *gloas.SignedBuilderRequestAuth, builders []frozenBuilderRef) {
	var pubkey phase0.BLSPubKey
	copy(pubkey[:], r.GetShare().ValidatorPubKey[:])
	entries := make([]*gloas.BuilderPreferencesEntry, 0, len(builders))
	for _, ref := range builders {
		entries = append(entries, &gloas.BuilderPreferencesEntry{
			ProposerPubKey:      pubkey,
			URL:                 ref.url,
			Auth:                signed,
			MaxExecutionPayment: ref.maxExecutionPayment,
		})
	}
	if err := r.beacon.SubmitBuilderPreferences(ctx, entries); err != nil {
		recordBuilderPreferencesSubmit(ctx, false)
		logger.Warn("builder preferences submit failed", fields.Slot(signed.Message.Slot), zap.Error(err))
		return
	}
	recordBuilderPreferencesSubmit(ctx, true)
}
