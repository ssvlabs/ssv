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
	tbftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/tbft"
	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/wire"
)

// proposer_tbft.go contains the TBFT-specific lifecycle hooks and
// supporting helpers for ProposerRunner. The runner only enters this
// path when constructed with a non-nil TBFTController; otherwise the
// existing QBFT path in proposer.go runs unchanged.

// MsgTypeTBFT is the SSV-internal MsgType for TBFT envelopes (Onion,
// NonReceipt, Candidate). The network layer must dispatch incoming
// SSVMessages with this type to ProcessTBFTEnvelope.
//
// NOTE: this is a placeholder value. Allocating a stable, ecosystem-
// wide MsgType byte is SSV-team coordination work — do not deploy with
// this value as-is. The constant exists here so the runner-side
// integration can compile and be exercised in tests.
const MsgTypeTBFT spectypes.MsgType = 0xF0

// proposerSlotDeadline is the upper bound on how long the TBFT driver
// goroutine may run for a given slot before the context is cancelled.
// The TBFT instance itself uses a tighter, layer-relative deadline
// (cfg.Deadline + gossip window) — this is just the outer-bound safety
// net so a stuck goroutine doesn't outlive the slot indefinitely.
const proposerSlotDeadline = 12 * time.Second

// tbftStartSlot kicks off the TBFT driver for `slot` in a goroutine and
// returns immediately. The goroutine drives Phases 1 → 2 → 3 across the
// slot duration, calling back into the lifecycle hooks defined below.
//
// Per-slot state (RANDAO sig, version, cancel) is stashed in r.tbftSlots
// keyed by slot — the FetchCandidate hook needs the RANDAO and the
// SubmitOutput hook needs the version.
func (r *ProposerRunner) tbftStartSlot(ctx context.Context, logger *zap.Logger, slot phase0.Slot, randaoSig spectypes.Signature) error {
	if !r.usesTBFT() {
		return fmt.Errorf("tbft start slot called with TBFT not configured")
	}

	var randao phase0.BLSSignature
	if len(randaoSig) != len(randao) {
		return fmt.Errorf("tbft start slot: unexpected RANDAO length %d (want %d)", len(randaoSig), len(randao))
	}
	copy(randao[:], randaoSig)

	slotStart := r.NetworkConfig.SlotStartTime(slot)
	slotCtx, cancel := context.WithDeadline(ctx, slotStart.Add(proposerSlotDeadline))

	state := &tbftSlotState{randao: randao, cancel: cancel}
	r.tbftMu.Lock()
	if existing, ok := r.tbftSlots[slot]; ok {
		// Defensive: a duplicate StartNewDuty for the same slot would
		// leak the previous goroutine. Cancel it before overwriting.
		existing.cancel()
	}
	r.tbftSlots[slot] = state
	r.tbftMu.Unlock()

	go func() {
		defer cancel()
		// Always finish the duty when the goroutine exits, regardless
		// of which path closed it (success, no-quorum, BuildAndBroadcast
		// error, ctx cancelled). markDutyFinished + EndDutyFlow are
		// idempotent — SubmitOutput/OnMissedSlot may have already
		// called them, this is the safety net.
		defer func() {
			r.markDutyFinished()
			r.measurements.EndDutyFlow()
		}()

		err := tbftadapter.RunProposerSlot(slotCtx, r.tbftCtrl, r.tbftSched, slot, slotStart)
		if err != nil {
			logger.Warn("TBFT slot failed", fields.Slot(slot), zap.Error(err))
		}

		// Only clean up if we're still the active slot state — a
		// later StartNewDuty for the same slot may have replaced us,
		// in which case the new goroutine owns slot's state and rate-
		// limit tracking.
		r.tbftMu.Lock()
		if r.tbftSlots[slot] == state {
			delete(r.tbftSlots, slot)
			r.tbftMu.Unlock()
			r.tbftRL.Forget(slot)
		} else {
			r.tbftMu.Unlock()
		}
	}()

	return nil
}

// ProcessTBFTEnvelope is the network-side entry point for TBFT messages.
// The network layer parses the SignedSSVMessage, authenticates the
// sender, and hands the inner data + sender ID here.
//
// Returns an error if the runner isn't on the TBFT path, if the rate
// limit is exceeded, or if the controller rejects the dispatched
// envelope. Errors are non-fatal at the runner level; the caller may
// log and drop.
func (r *ProposerRunner) ProcessTBFTEnvelope(senderID spectypes.OperatorID, data []byte) error {
	if !r.usesTBFT() {
		return fmt.Errorf("tbft: runner not configured for TBFT consensus")
	}

	env, err := wire.Unwrap(data)
	if err != nil {
		return fmt.Errorf("tbft: unwrap envelope: %w", err)
	}

	switch env.Kind {
	case wire.KindOnion:
		if err := r.tbftRL.AllowOnion(phase0.Slot(env.Onion.Height), senderID); err != nil {
			return err
		}
	case wire.KindNonReceipt:
		if err := r.tbftRL.AllowNonReceipt(phase0.Slot(env.NonReceipt.Height), senderID, env.NonReceipt.Layer); err != nil {
			return err
		}
	case wire.KindCandidate:
		if err := r.tbftRL.AllowCandidate(phase0.Slot(env.Candidate.Height), senderID, env.Candidate.Layer); err != nil {
			return err
		}
	}

	return tbftadapter.DispatchEnvelope(r.tbftCtrl, env)
}

// ---- Lifecycle hooks (registered with the Scheduler in NewProposerRunner)

// tbftFetchCandidate is invoked by the Scheduler when this operator is
// the designated leader for some `layer` of `slot`, at the layer's
// FetchAt offset. It fetches a beacon block, blinds it, and returns the
// SSZ-encoded candidate value. The version is captured into the slot's
// state so SubmitOutput can decode the agreed-upon block back into a
// versioned proposal.
//
// All layer leaders fetch the same kind of block (same RANDAO, same
// graffiti); a more sophisticated runner may vary timing or block
// source per layer. The protocol treats the value bytes as opaque, so
// the wire format prepends a 1-byte version tag for SubmitOutput.
func (r *ProposerRunner) tbftFetchCandidate(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
	r.tbftMu.Lock()
	state := r.tbftSlots[slot]
	r.tbftMu.Unlock()
	if state == nil {
		return nil, fmt.Errorf("tbft: no active state for slot %d (FetchCandidate)", slot)
	}

	vBlk, _, err := r.GetBeaconNode().GetBeaconBlock(ctx, slot, r.graffiti, state.randao[:])
	if err != nil {
		return nil, fmt.Errorf("tbft: get beacon block (slot=%d layer=%d): %w", slot, layer, err)
	}

	_, blindedMarshaler, err := blindutil.EnsureBlinded(vBlk)
	if err != nil {
		return nil, fmt.Errorf("tbft: ensure blinded: %w", err)
	}
	blindedSSZ, err := blindedMarshaler.MarshalSSZ()
	if err != nil {
		return nil, fmt.Errorf("tbft: marshal blinded block: %w", err)
	}

	// Cache version + (optionally) the original full block for this
	// operator's local SubmitOutput path. If this operator wins the
	// slot AND fetched the agreed-upon layer, we can submit the full
	// block (with blobs) instead of the blinded block.
	r.tbftMu.Lock()
	if r.tbftSlots[slot] == state {
		state.version = vBlk.Version
	}
	r.tbftMu.Unlock()

	if !vBlk.Blinded {
		r.cachedFullBlock = vBlk
		r.cachedBlindedBlockSSZ = blindedSSZ
	}

	return encodeTBFTCandidate(vBlk.Version, blindedSSZ), nil
}

// tbftBroadcast wraps the TBFT envelope bytes in an operator-signed
// SSVMessage and pushes them through SSV's existing p2p layer.
func (r *ProposerRunner) tbftBroadcast(ctx context.Context, slot phase0.Slot, data []byte) error {
	msgID := spectypes.NewMsgID(r.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.RunnerRoleType)
	ssvMsg := &spectypes.SSVMessage{
		MsgType: MsgTypeTBFT,
		MsgID:   msgID,
		Data:    data,
	}
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("tbft broadcast: sign envelope: %w", err)
	}
	signed := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}
	if err := r.GetNetwork().Broadcast(msgID, signed); err != nil {
		return fmt.Errorf("tbft broadcast: publish: %w", err)
	}
	return nil
}

// tbftSubmitOutput is invoked when TBFT decides on a layer's value. It
// applies the same doppelganger / slashing-protection check as the
// existing QBFT post-consensus submit, decodes the candidate-value
// bytes into a versioned proposal, and submits to the beacon node.
//
// If this operator originally fetched a full (non-blinded) block whose
// blinded form matches the decided value, we submit the full block (so
// blobs propagate through this operator). Otherwise we submit the
// blinded block — every operator in the cluster has the bytes.
func (r *ProposerRunner) tbftSubmitOutput(ctx context.Context, slot phase0.Slot, output *tbftcore.Output) error {
	if output == nil {
		return fmt.Errorf("tbft submit: nil output")
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("tbft submit: current validator duty: %w", err)
	}
	if !r.doppelgangerHandler.CanSign(duty.ValidatorIndex) {
		// Match the existing QBFT path's behaviour: log + skip, not
		// error. The slot is missed; the runner doesn't keep retrying.
		return nil
	}

	r.tbftMu.Lock()
	state := r.tbftSlots[slot]
	r.tbftMu.Unlock()

	version, blindedSSZ, err := decodeTBFTCandidate(output.Value)
	if err != nil {
		return fmt.Errorf("tbft submit: decode candidate value: %w", err)
	}

	// Fall back to the slot's locally-fetched version if the candidate
	// doesn't carry one (it always should under the canonical encoding,
	// but be defensive).
	if version == spec.DataVersionUnknown && state != nil {
		version = state.version
	}
	if version == spec.DataVersionUnknown {
		return fmt.Errorf("tbft submit: cannot determine spec version for slot %d", slot)
	}

	vBlk, err := decodeBlindedProposal(version, blindedSSZ)
	if err != nil {
		return fmt.Errorf("tbft submit: decode blinded proposal: %w", err)
	}

	// If our locally-cached full block matches the decided value, prefer
	// it (so blobs propagate through this operator). Match is byte-
	// equality on the blinded SSZ — same check as the QBFT path uses
	// at ProcessPostConsensus.
	if r.cachedFullBlock != nil && len(r.cachedBlindedBlockSSZ) > 0 &&
		bytes.Equal(blindedSSZ, r.cachedBlindedBlockSSZ) {
		vBlk = r.cachedFullBlock
	}

	var specSig phase0.BLSSignature
	copy(specSig[:], output.Signature)

	r.doppelgangerHandler.ReportQuorum(duty.ValidatorIndex)

	if err := r.GetBeaconNode().SubmitBeaconBlock(ctx, vBlk, specSig); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleProposer)
		return fmt.Errorf("tbft submit: submit beacon block: %w", err)
	}

	if currentDutySlot, err := r.currentDutySlot(); err == nil {
		recordSuccessfulSubmission(ctx, 1, r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot), spectypes.BNRoleProposer)
	}
	return nil
}

// tbftOnMissedSlot is invoked when the TBFT instance fails to reach
// quorum at any layer (typically tbftcore.ErrNoQuorum). The metric
// is recorded here; the duty-finished bookkeeping happens once in the
// driver goroutine's cleanup defer.
func (r *ProposerRunner) tbftOnMissedSlot(ctx context.Context, slot phase0.Slot, reason error) {
	recordFailedSubmission(ctx, spectypes.BNRoleProposer)
}

// ---- Wire helpers ----

// encodeTBFTCandidate produces the candidate-value bytes that travel
// through the TBFT protocol: a 1-byte spec.DataVersion tag followed by
// the SSZ-encoded blinded proposal. The protocol treats the bytes as
// opaque; the runner pair (FetchCandidate, SubmitOutput) is the only
// code that interprets them.
func encodeTBFTCandidate(version spec.DataVersion, blindedSSZ []byte) []byte {
	out := make([]byte, 1+len(blindedSSZ))
	out[0] = byte(version)
	copy(out[1:], blindedSSZ)
	return out
}

// decodeTBFTCandidate is the inverse of encodeTBFTCandidate. Returns an
// error on empty input.
func decodeTBFTCandidate(value []byte) (spec.DataVersion, []byte, error) {
	if len(value) < 1 {
		return spec.DataVersionUnknown, nil, fmt.Errorf("empty candidate value")
	}
	return spec.DataVersion(value[0]), value[1:], nil
}

// decodeBlindedProposal turns a (version, blinded SSZ) pair back into a
// *api.VersionedProposal. Reuses the spec's `ValidatorConsensusData.GetBlockData`
// dispatch so all version-specific decoding lives in one place. The
// Duty field on the synthetic ValidatorConsensusData is unused by
// GetBlockData; only Version + DataSSZ matter.
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

