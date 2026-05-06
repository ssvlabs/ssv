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
// has been verified.
func (r *ProposerRunner) ProcessOBFTEnvelopeMsg(ctx context.Context, senderID spectypes.OperatorID, env *wire.Envelope) error {
	if env == nil {
		return fmt.Errorf("obft: nil envelope")
	}

	// Rate-limit per kind.
	switch env.Kind {
	case wire.KindPhase1Bundle:
		if err := r.obftRL.AllowPhase1Bundle(phase0.Slot(env.Phase1Bundle.Height), senderID, env.Phase1Bundle.Layer); err != nil {
			return err
		}
	case wire.KindOnion:
		// Multi-emit allowed up to K per (slot, op).
		cfg, ok := r.obftCtrl.GetInstance(phase0.Slot(env.Onion.Height))
		k := obftadapter.DefaultK
		if ok && cfg != nil && cfg.Config != nil {
			k = cfg.Config.K()
		}
		if err := r.obftRL.AllowOnion(phase0.Slot(env.Onion.Height), senderID, k); err != nil {
			return err
		}
	case wire.KindNR:
		if err := r.obftRL.AllowNR(phase0.Slot(env.NR.Height), senderID); err != nil {
			return err
		}
	case wire.KindCertificate:
		if err := r.obftRL.AllowCertificate(phase0.Slot(env.Certificate.Height), senderID); err != nil {
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
	}
	r.obftMu.Unlock()

	if !vBlk.Blinded {
		r.cachedFullBlock = vBlk
		r.cachedBlindedBlockSSZ = blindedSSZ
	}

	return encodeOBFTCandidate(vBlk.Version, blindedSSZ), nil
}

// obftHostValidate is invoked by the Scheduler when a peer's Phase-1 bundle
// has been observed; the host returns the valid/not-valid verdict on the
// bundled value. For SSV proposer duty, this is where parent_root /
// fork-domain / slashing-protection checks would land.
//
// First-cut implementation: trust the bundle's value as-is (return true).
// The application-level validity machinery is shared with the QBFT path's
// value-checker; integration is out of scope for this initial migration
// commit and is tracked as a follow-up.
func (r *ProposerRunner) obftHostValidate(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error) {
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

	r.obftMu.Lock()
	state := r.obftSlots[slot]
	r.obftMu.Unlock()

	version, blindedSSZ, err := decodeOBFTCandidate(output.Value)
	if err != nil {
		return fmt.Errorf("obft submit: decode candidate value: %w", err)
	}

	if version == spec.DataVersionUnknown && state != nil {
		version = state.version
	}
	if version == spec.DataVersionUnknown {
		return fmt.Errorf("obft submit: cannot determine spec version for slot %d", slot)
	}

	vBlk, err := decodeBlindedProposal(version, blindedSSZ)
	if err != nil {
		return fmt.Errorf("obft submit: decode blinded proposal: %w", err)
	}

	if r.cachedFullBlock != nil && len(r.cachedBlindedBlockSSZ) > 0 &&
		bytes.Equal(blindedSSZ, r.cachedBlindedBlockSSZ) {
		vBlk = r.cachedFullBlock
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
