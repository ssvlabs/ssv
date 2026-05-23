package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	twoabwire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft/twoab"
)

// proposer_twoab.go contains the 2abOBFT-specific lifecycle glue for
// ProposerRunner, the sibling of proposer_obft.go. The runner only enters this
// path when constructed with a non-nil TwoabController (variant == variantTwoab).
//
// Beacon-side hooks (FetchCandidate / HostValidate / SubmitOutput /
// OnMissedSlot) and the per-slot scratch (obftSlots) are shared with the OBFT
// path — they operate on the shared obft.Output type and the variant-identical
// candidate encoding. Only the consensus-driver wiring lives here: the slot
// driver, inbound dispatch (five wire kinds, incl. the Phase-2a Value/NoValue
// envelopes), the distinct broadcast MsgType, and the inner-signer check.

// twoabStartSlot kicks off the 2abOBFT driver for `slot` in a goroutine and
// returns immediately. Mirror of obftStartSlot.
func (r *ProposerRunner) twoabStartSlot(ctx context.Context, logger *zap.Logger, slot phase0.Slot, randaoSig spectypes.Signature) error {
	sharePubkey := phase0.BLSPubKey(r.GetShare().SharePubKey)
	if err := r.signer.IsBeaconBlockSlashable(sharePubkey, slot); err != nil {
		logger.Warn("2abOBFT slot skipped: slashing protection",
			fields.Slot(slot), zap.Error(err))
		return nil
	}

	var randao phase0.BLSSignature
	if len(randaoSig) != len(randao) {
		return fmt.Errorf("2abobft start slot: unexpected RANDAO length %d (want %d)", len(randaoSig), len(randao))
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

		err := twoabadapter.RunProposerSlot(slotCtx, r.twoabSched, slot, slotStart)
		if err != nil {
			logger.Warn("2abOBFT slot failed", fields.Slot(slot), zap.Error(err))
		}

		r.obftMu.Lock()
		if r.obftSlots[slot] == state {
			delete(r.obftSlots, slot)
			r.obftMu.Unlock()
			r.twoabRL.Forget(slot)
			// Pre-instance envelope cleanup is handled atomically by
			// Controller.EndInstance (drain + the endedSlots fence on
			// BufferEnvelope); no separate ForgetPending call needed.
		} else {
			r.obftMu.Unlock()
		}
	}()

	return nil
}

// ProcessTwoabEnvelopeMsg is the network-side entry point for 2abOBFT messages.
// Called by the validator's dispatch layer after the outer SignedSSVMessage has
// been verified. `rawWireBytes` is the original SSVMessage.Data, hashed directly
// by the rate limiter. Mirror of ProcessOBFTEnvelopeMsg over the five 2ab kinds.
func (r *ProposerRunner) ProcessTwoabEnvelopeMsg(ctx context.Context, senderID spectypes.OperatorID, env *twoabwire.Envelope, rawWireBytes []byte) error {
	if env == nil {
		return fmt.Errorf("2abobft: nil envelope")
	}
	// Variant guard: this entry point is reached purely by SSVMessage type
	// (the validator dispatch doesn't cross-check the runner's variant), so a
	// 2ab envelope mis-delivered to an OBFT-variant runner would nil-deref the
	// twoab machinery. Reject cleanly instead — the runner stays sound even if
	// the validation layer is bypassed.
	if r.variant != variantTwoab {
		return fmt.Errorf("2abobft: envelope routed to a non-2abOBFT-variant proposer runner")
	}

	// Defense-in-depth: the inner claimed signer must match the outer SSV
	// signer (the validation layer checks this too; duplicated so the runner is
	// sound even if validation is bypassed).
	if err := checkInner2abSignerMatchesOuter(env, senderID); err != nil {
		return err
	}

	// Rate-limit by content hash: identical gossipsub redeliveries are dropped;
	// distinct content from the same operator is admitted so the protocol's
	// equivocation paths fire. Hash over rawWireBytes (the authenticated wire
	// bytes that produced env) to avoid a re-encode allocation.
	switch env.Kind {
	case twoabwire.KindPhase1Bundle:
		if err := r.twoabRL.AllowPhase1Bundle(phase0.Slot(env.Phase1Bundle.Height), senderID, env.Phase1Bundle.Layer, rawWireBytes); err != nil {
			return err
		}
	case twoabwire.KindValue:
		if err := r.twoabRL.AllowValueMsg(phase0.Slot(env.ValueMsg.Height), senderID, rawWireBytes); err != nil {
			return err
		}
	case twoabwire.KindNoValue:
		if err := r.twoabRL.AllowNoValueMsg(phase0.Slot(env.NoValueMsg.Height), senderID, rawWireBytes); err != nil {
			return err
		}
	case twoabwire.KindCommit:
		if err := r.twoabRL.AllowCommit(phase0.Slot(env.Commit.Height), senderID, rawWireBytes); err != nil {
			return err
		}
	case twoabwire.KindCertificate:
		if err := r.twoabRL.AllowCertificate(phase0.Slot(env.Certificate.Height), senderID, rawWireBytes); err != nil {
			return err
		}
	}

	// Compute observedOffset for the Phase-1 bundle acceptance window.
	observedOffset := time.Duration(0)
	if env.Kind == twoabwire.KindPhase1Bundle {
		r.obftMu.Lock()
		state := r.obftSlots[phase0.Slot(env.Phase1Bundle.Height)]
		r.obftMu.Unlock()
		if state != nil {
			observedOffset = time.Since(state.slotStart)
		}
	}

	return twoabadapter.DispatchEnvelope(ctx, r.twoabSched, env, observedOffset)
}

// twoabBroadcast wraps the 2abOBFT envelope bytes in an operator-signed
// SSVMessage (tagged SSV2abOBFTMsgType) and publishes via SSV's p2p layer.
// Mirror of obftBroadcast; the distinct MsgType routes peers to
// ProcessTwoabEnvelopeMsg.
func (r *ProposerRunner) twoabBroadcast(ctx context.Context, slot phase0.Slot, data []byte) error {
	msgID := spectypes.NewMsgID(r.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.RunnerRoleType)
	ssvMsg := &spectypes.SSVMessage{
		MsgType: ssvmessage.SSV2abOBFTMsgType,
		MsgID:   msgID,
		Data:    data,
	}
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("2abobft broadcast: sign envelope: %w", err)
	}
	signed := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}
	if err := r.GetNetwork().Broadcast(msgID, signed); err != nil {
		return fmt.Errorf("2abobft broadcast: publish: %w", err)
	}
	return nil
}

// twoabBroadcastCertificate gossips a 2abOBFT final certificate after Resolve
// produces an Output. Mirror of obftBroadcastCertificate.
func (r *ProposerRunner) twoabBroadcastCertificate(ctx context.Context, slot phase0.Slot, data []byte) error {
	return r.twoabBroadcast(ctx, slot, data)
}

// twoabOnReplayError surfaces non-fatal errors from DrainPending (envelopes
// buffered before this operator's instance started, replayed at slot start).
// Mirror of obftOnReplayError with the 2ab wire kind.
func (r *ProposerRunner) twoabOnReplayError(_ context.Context, slot phase0.Slot, kind twoabwire.MessageKind, err error) {
	zap.L().Debug("2abobft pending-envelope replay error",
		fields.Slot(slot),
		zap.Uint8("kind", uint8(kind)),
		zap.Error(err),
	)
}

// checkInner2abSignerMatchesOuter rejects a 2abOBFT envelope whose claimed
// inner OperatorID doesn't match the outer SSV signer. Mirror of
// checkInnerSignerMatchesOuter over the 2ab kinds (Certificate carries no inner
// OperatorID, so the outer signer is its only identity binding).
func checkInner2abSignerMatchesOuter(env *twoabwire.Envelope, outerSigner spectypes.OperatorID) error {
	mismatch := func(inner twoabcore.OperatorID) error {
		return fmt.Errorf("2abobft dispatch: outer signer %d != inner OperatorID %d", outerSigner, inner)
	}
	switch env.Kind {
	case twoabwire.KindPhase1Bundle:
		if env.Phase1Bundle == nil {
			return fmt.Errorf("2abobft dispatch: nil Phase1Bundle body")
		}
		if twoabcore.OperatorID(outerSigner) != env.Phase1Bundle.OperatorID {
			return mismatch(env.Phase1Bundle.OperatorID)
		}
	case twoabwire.KindValue:
		if env.ValueMsg == nil {
			return fmt.Errorf("2abobft dispatch: nil ValueMsg body")
		}
		if twoabcore.OperatorID(outerSigner) != env.ValueMsg.OperatorID {
			return mismatch(env.ValueMsg.OperatorID)
		}
	case twoabwire.KindNoValue:
		if env.NoValueMsg == nil {
			return fmt.Errorf("2abobft dispatch: nil NoValueMsg body")
		}
		if twoabcore.OperatorID(outerSigner) != env.NoValueMsg.OperatorID {
			return mismatch(env.NoValueMsg.OperatorID)
		}
	case twoabwire.KindCommit:
		if env.Commit == nil {
			return fmt.Errorf("2abobft dispatch: nil Commit body")
		}
		if twoabcore.OperatorID(outerSigner) != env.Commit.OperatorID {
			return mismatch(env.Commit.OperatorID)
		}
	case twoabwire.KindCertificate:
		// Certificate has no inner OperatorID; outer signer is the only binding.
	}
	return nil
}
