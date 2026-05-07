package runner

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	blindutil "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/blind"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/wire"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft"
)

// proposer_obft.go contains the OBFT-specific lifecycle hooks and supporting
// helpers for ProposerRunner. The runner only enters this path when
// constructed with a non-nil OBFTController; otherwise an error.

// proposerSlotDeadline is the upper bound on how long the OBFT driver
// goroutine may run for a given slot before the context is canceled. The
// OBFT instance itself uses a tighter, layer-relative deadline (cfg
// timing) — this is just the outer-bound safety net.
const proposerSlotDeadline = 12 * time.Second

// obftStartSlot kicks off the OBFT driver for `slot` in a goroutine and
// returns immediately.
func (r *ProposerRunner) obftStartSlot(ctx context.Context, logger *zap.Logger, slot phase0.Slot, randaoSig spectypes.Signature) error {
	sharePubkey := phase0.BLSPubKey(r.GetShare().SharePubKey)
	if err := r.signer.IsBeaconBlockSlashable(sharePubkey, slot); err != nil {
		logger.Warn("OBFT slot skipped: slashing protection",
			fields.Slot(slot), zap.Error(err))
		return nil
	}

	var randao phase0.BLSSignature
	if len(randaoSig) != len(randao) {
		return fmt.Errorf("obft start slot: unexpected RANDAO length %d (want %d)", len(randaoSig), len(randao))
	}
	copy(randao[:], randaoSig)

	slotStart := r.NetworkConfig.SlotStartTime(slot)
	slotCtx, cancel := context.WithDeadline(ctx, slotStart.Add(proposerSlotDeadline))

	state := &obftSlotState{randao: randao, cancel: cancel, slotStart: slotStart}
	r.obftMu.Lock()
	if existing, ok := r.obftSlots[slot]; ok {
		existing.cancel()
	}
	r.obftSlots[slot] = state
	r.obftMu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			r.markDutyFinished()
			r.measurements.EndDutyFlow()
		}()

		err := obftadapter.RunProposerSlot(slotCtx, r.obftSched, slot, slotStart)
		if err != nil {
			logger.Warn("OBFT slot failed", fields.Slot(slot), zap.Error(err))
		}

		r.obftMu.Lock()
		if r.obftSlots[slot] == state {
			delete(r.obftSlots, slot)
			r.obftMu.Unlock()
			r.obftRL.Forget(slot)
		} else {
			r.obftMu.Unlock()
		}
	}()

	return nil
}

// ProcessOBFTEnvelopeMsg is the network-side entry point for OBFT messages.
// Called by the validator's dispatch layer after the outer SignedSSVMessage
// has been verified. `rawWireBytes` is the original SSVMessage.Data — passed
// through so the rate limiter can hash the bytes directly instead of
// re-encoding the decoded envelope.
func (r *ProposerRunner) ProcessOBFTEnvelopeMsg(ctx context.Context, senderID spectypes.OperatorID, env *wire.Envelope, rawWireBytes []byte) error {
	if env == nil {
		return fmt.Errorf("obft: nil envelope")
	}

	// Defense-in-depth: enforce that the inner-message claimed signer matches
	// the outer SSV signer. Without this, a byzantine X could broadcast a
	// Commit{OperatorID: Y, ...} signed under X's identity; receivers would
	// then attribute Y's evidence (Rule 3 cross-onion, Rule 1 cross-signing)
	// against Y for behavior Y did not exhibit. The validation layer also
	// checks this (verifyInnerSignerMatchesOuter); duplicating here so the
	// runner is sound even if validation is bypassed in tests / future paths.
	if err := checkInnerSignerMatchesOuter(env, senderID); err != nil {
		return err
	}

	// Rate-limit by content hash. Identical bytes redelivered by gossipsub
	// are dropped; distinct content from the same operator is admitted so
	// the protocol's equivocation paths fire (Rule 2 on second distinct
	// Phase-1 bundle, Rule 3 on second distinct KindCommit). A naive
	// "≤ 1 per (slot, op)" rate limit would silently swallow the second
	// distinct emission and prevent attribution.
	//
	// Hash directly over rawWireBytes — these are the authenticated wire
	// bytes that produced env, so hashing them is equivalent to hashing
	// re-encoded env (the wire format is deterministic) but avoids the
	// re-encode allocation.
	switch env.Kind {
	case wire.KindPhase1Bundle:
		if err := r.obftRL.AllowPhase1Bundle(phase0.Slot(env.Phase1Bundle.Height), senderID, env.Phase1Bundle.Layer, rawWireBytes); err != nil {
			return err
		}
	case wire.KindCommit:
		if err := r.obftRL.AllowCommit(phase0.Slot(env.Commit.Height), senderID, rawWireBytes); err != nil {
			return err
		}
	case wire.KindCertificate:
		if err := r.obftRL.AllowCertificate(phase0.Slot(env.Certificate.Height), senderID, rawWireBytes); err != nil {
			return err
		}
	}

	// Compute observedOffset for Phase-1 bundle acceptance window.
	observedOffset := time.Duration(0)
	if env.Kind == wire.KindPhase1Bundle {
		r.obftMu.Lock()
		state := r.obftSlots[phase0.Slot(env.Phase1Bundle.Height)]
		r.obftMu.Unlock()
		if state != nil {
			observedOffset = time.Since(state.slotStart)
		}
	}

	return obftadapter.DispatchEnvelope(ctx, r.obftSched, env, observedOffset)
}

// ---- Lifecycle hooks (registered with the Scheduler in NewProposerRunner)

// obftFetchCandidate is invoked by the Scheduler when this operator is the
// designated leader for some `layer` of `slot`, at the layer's FetchAt
// offset.
func (r *ProposerRunner) obftFetchCandidate(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
	r.obftMu.Lock()
	state := r.obftSlots[slot]
	r.obftMu.Unlock()
	if state == nil {
		return nil, fmt.Errorf("obft: no active state for slot %d (FetchCandidate)", slot)
	}

	vBlk, _, err := r.GetBeaconNode().GetBeaconBlock(ctx, slot, r.graffiti, state.randao[:])
	if err != nil {
		return nil, fmt.Errorf("obft: get beacon block (slot=%d layer=%d): %w", slot, layer, err)
	}

	_, blindedMarshaler, err := blindutil.EnsureBlinded(vBlk)
	if err != nil {
		return nil, fmt.Errorf("obft: ensure blinded: %w", err)
	}
	blindedSSZ, err := blindedMarshaler.MarshalSSZ()
	if err != nil {
		return nil, fmt.Errorf("obft: marshal blinded block: %w", err)
	}

	r.obftMu.Lock()
	if r.obftSlots[slot] == state {
		state.version = vBlk.Version
		// Cache the full block + its blinded fingerprint per-slot under the
		// lock, so SubmitOutput (running on a different goroutine) reads a
		// consistent snapshot AND so the previous slot's block can't leak
		// into the next slot's submit (state is per-slot, evicted on duty
		// completion).
		if !vBlk.Blinded {
			state.cachedFullBlock = vBlk
			state.cachedBlindedSSZ = blindedSSZ
		}
	}
	r.obftMu.Unlock()

	return encodeOBFTCandidate(vBlk.Version, blindedSSZ), nil
}

// obftHostValidate is invoked by the Scheduler when a peer's Phase-1 bundle
// has been observed (and on Certificate fast-path); the host returns the
// valid/not-valid verdict on the bundled value.
//
// Current checks (cheap, structural):
//   - Candidate value decodes as a [version | blinded SSZ] pair
//   - Blinded proposal decodes for the declared spec version
//   - Decoded proposal's Slot equals the argued slot
//
// TODO: integrate with the proposer's value-checker for parent_root /
// fork-domain / slashing-protection (QBFT proposer-flow has these via
// `ProposerValueCheckF` in ssv-spec). Until then a byzantine that can
// produce a self-consistent (but on-the-wrong-fork) blinded block can
// still pass this gate; downstream beacon-node submission would reject it
// but the slot is still a miss.
//
// `layer` may be -1 when called from the Certificate fast-path (the cert
// carries V+sig but not the originating layer); callers should not key on
// it.
func (r *ProposerRunner) obftHostValidate(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error) {
	version, blindedSSZ, err := decodeOBFTCandidate(value)
	if err != nil {
		return false, nil // malformed envelope (not error: just invalid V)
	}
	if version == spec.DataVersionUnknown {
		return false, nil
	}
	vBlk, err := decodeBlindedProposal(version, blindedSSZ)
	if err != nil {
		return false, nil
	}
	proposalSlot, err := vBlk.Slot()
	if err != nil {
		return false, nil
	}
	if proposalSlot != slot {
		return false, nil
	}
	return true, nil
}

// obftBroadcast wraps the OBFT envelope bytes in an operator-signed
// SSVMessage and pushes them through SSV's existing p2p layer.
func (r *ProposerRunner) obftBroadcast(ctx context.Context, slot phase0.Slot, data []byte) error {
	msgID := spectypes.NewMsgID(r.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.RunnerRoleType)
	ssvMsg := &spectypes.SSVMessage{
		MsgType: ssvmessage.SSVOBFTMsgType,
		MsgID:   msgID,
		Data:    data,
	}
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("obft broadcast: sign envelope: %w", err)
	}
	signed := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}
	if err := r.GetNetwork().Broadcast(msgID, signed); err != nil {
		return fmt.Errorf("obft broadcast: publish: %w", err)
	}
	return nil
}

// obftSubmitOutput is invoked when OBFT decides on a layer's value. It
// applies the same doppelganger / slashing-protection check as the existing
// QBFT post-consensus submit, decodes the candidate-value bytes into a
// versioned proposal, and submits to the beacon node.
func (r *ProposerRunner) obftSubmitOutput(ctx context.Context, slot phase0.Slot, output *obftcore.Output) error {
	if output == nil {
		return fmt.Errorf("obft submit: nil output")
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("obft submit: current validator duty: %w", err)
	}
	if !r.doppelgangerHandler.CanSign(duty.ValidatorIndex) {
		return nil
	}

	// Snapshot the per-slot state under the lock. Holding all reads (version
	// + cached block + cached SSZ) inside one critical section so a
	// concurrent fetch on the same slot can't mutate them mid-read. The
	// per-slot scoping prevents a previous slot's cached block from being
	// seen here.
	r.obftMu.Lock()
	state := r.obftSlots[slot]
	var (
		stateVersion     spec.DataVersion
		cachedFullBlock  *api.VersionedProposal
		cachedBlindedSSZ []byte
	)
	if state != nil {
		stateVersion = state.version
		cachedFullBlock = state.cachedFullBlock
		cachedBlindedSSZ = state.cachedBlindedSSZ
	}
	r.obftMu.Unlock()

	version, blindedSSZ, err := decodeOBFTCandidate(output.Value)
	if err != nil {
		return fmt.Errorf("obft submit: decode candidate value: %w", err)
	}

	if version == spec.DataVersionUnknown && state != nil {
		version = stateVersion
	}
	if version == spec.DataVersionUnknown {
		return fmt.Errorf("obft submit: cannot determine spec version for slot %d", slot)
	}

	vBlk, err := decodeBlindedProposal(version, blindedSSZ)
	if err != nil {
		return fmt.Errorf("obft submit: decode blinded proposal: %w", err)
	}

	if cachedFullBlock != nil && len(cachedBlindedSSZ) > 0 &&
		bytes.Equal(blindedSSZ, cachedBlindedSSZ) {
		vBlk = cachedFullBlock
	}

	var specSig phase0.BLSSignature
	copy(specSig[:], output.Signature)

	sharePubkey := phase0.BLSPubKey(r.GetShare().SharePubKey)
	if err := r.signer.IsBeaconBlockSlashable(sharePubkey, slot); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposer)
		return fmt.Errorf("obft submit: slashable block for slot %d: %w", slot, err)
	}
	if err := r.signer.UpdateHighestProposal(sharePubkey, slot); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposer)
		return fmt.Errorf("obft submit: update highest proposal: %w", err)
	}

	r.doppelgangerHandler.ReportQuorum(duty.ValidatorIndex)

	if err := r.GetBeaconNode().SubmitBeaconBlock(ctx, vBlk, specSig); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposer)
		return fmt.Errorf("obft submit: submit beacon block: %w", err)
	}

	if currentDutySlot, err := r.currentDutySlot(); err == nil {
		recordSuccessfulSubmission(ctx, 1, r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot), spectypes.BNRoleProposer)
	}
	return nil
}

// obftBroadcastCertificate gossips an OBFT final certificate after Resolve
// produces an Output. Receivers without local reconstruction can use the
// certificate to submit (V, S) downstream.
func (r *ProposerRunner) obftBroadcastCertificate(ctx context.Context, slot phase0.Slot, data []byte) error {
	return r.obftBroadcast(ctx, slot, data)
}

// obftOnMissedSlot is invoked when OBFT fails to reach quorum at any layer.
func (r *ProposerRunner) obftOnMissedSlot(ctx context.Context, slot phase0.Slot, reason error) {
	recordFailedSubmission(ctx, spectypes.BNRoleProposer)
}

// obftOnReplayError surfaces non-fatal errors from DrainPending — envelopes
// buffered before this operator's instance had started, replayed at slot
// start. These errors don't fail the slot but are useful telemetry for
// detecting peers broadcasting malformed envelopes pre-StartNewInstance.
// Logged at debug to avoid noise; bumping a metric counter would be a
// future improvement.
func (r *ProposerRunner) obftOnReplayError(_ context.Context, slot phase0.Slot, kind wire.MessageKind, err error) {
	zap.L().Debug("obft pending-envelope replay error",
		fields.Slot(slot),
		zap.Uint8("kind", uint8(kind)),
		zap.Error(err),
	)
}

// checkInnerSignerMatchesOuter rejects an envelope whose claimed inner
// OperatorID doesn't match the outer SSV signer. Mirrors the validation
// layer's verifyInnerSignerMatchesOuter (defense-in-depth at dispatch).
func checkInnerSignerMatchesOuter(env *wire.Envelope, outerSigner spectypes.OperatorID) error {
	switch env.Kind {
	case wire.KindPhase1Bundle:
		if env.Phase1Bundle == nil {
			return fmt.Errorf("obft dispatch: nil Phase1Bundle body")
		}
		if obftcore.OperatorID(outerSigner) != env.Phase1Bundle.OperatorID {
			return fmt.Errorf("obft dispatch: outer signer %d != inner OperatorID %d",
				outerSigner, env.Phase1Bundle.OperatorID)
		}
	case wire.KindCommit:
		if env.Commit == nil {
			return fmt.Errorf("obft dispatch: nil Commit body")
		}
		if obftcore.OperatorID(outerSigner) != env.Commit.OperatorID {
			return fmt.Errorf("obft dispatch: outer signer %d != inner OperatorID %d",
				outerSigner, env.Commit.OperatorID)
		}
	case wire.KindCertificate:
		// Certificate has no inner OperatorID; outer signer is the only
		// identity binding.
	}
	return nil
}

// ---- Wire helpers ----

func encodeOBFTCandidate(version spec.DataVersion, blindedSSZ []byte) []byte {
	out := make([]byte, 1+len(blindedSSZ))
	out[0] = byte(version)
	copy(out[1:], blindedSSZ)
	return out
}

func decodeOBFTCandidate(value []byte) (spec.DataVersion, []byte, error) {
	if len(value) < 1 {
		return spec.DataVersionUnknown, nil, fmt.Errorf("empty candidate value")
	}
	return spec.DataVersion(value[0]), value[1:], nil
}

func decodeBlindedProposal(version spec.DataVersion, blindedSSZ []byte) (*api.VersionedProposal, error) {
	cd := &spectypes.ValidatorConsensusData{
		Version: version,
		DataSSZ: blindedSSZ,
	}
	vBlk, _, err := cd.GetBlockData()
	if err != nil {
		return nil, err
	}
	return vBlk, nil
}
