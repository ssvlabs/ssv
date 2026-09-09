package validation

// envelope_dissemination.go validates SSVEnvelopeDisseminationMsgType messages (SIP #94 §6/§7): the
// builder operator's broadcast of the blinded execution-payload envelope the cluster threshold-signs.
// Validation is content-agnostic — the §6 checks binding the envelope to the decided block are runner
// concerns — so the rules here are structure, metadata, and the per-signer dissemination budget.

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func (mv *messageValidator) validateEnvelopeDisseminationMessage(
	ctx context.Context,
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	topic string,
	_ peer.ID, // the relaying peer plays no part: a repeat is IGNORE'd whichever peer relays it
	receivedAt time.Time,
) (
	*spectypes.EnvelopeDissemination,
	error,
) {
	ssvMessage := signedSSVMessage.SSVMessage
	role := ssvMessage.GetID().GetRoleType()

	// Rule: dissemination messages are admitted only for the envelope-proposer role.
	if role != spectypes.RoleEnvelopeProposer {
		e := ErrUnexpectedEnvelopeDissemination
		e.got = fmt.Sprintf("%v (%d)", role, role)
		return nil, e
	}

	// Rule: the carrier and its blinded envelope must decode (within the SSVMessage.Data cap enforced
	// upstream), so undecodable bytes cannot consume the signer's dissemination budget.
	dissemination := &spectypes.EnvelopeDissemination{}
	if err := dissemination.Decode(ssvMessage.Data); err != nil {
		e := ErrUndecodableMessageData
		e.innerErr = err
		return nil, e
	}
	if dissemination.Envelope == nil || dissemination.Envelope.ExecutionRequests == nil {
		e := ErrUndecodableMessageData
		e.innerErr = fmt.Errorf("envelope dissemination carries no blinded envelope")
		return nil, e
	}
	slot := dissemination.Slot

	if err := mv.validateTopicAtSlot(committeeInfo, topic, slot); err != nil {
		return dissemination, err
	}
	if err := mv.validateDomainAtSlot(ssvMessage.GetID(), slot); err != nil {
		return dissemination, err
	}

	// Rule: the role must exist at the message slot (the Gloas fork gate).
	if !mv.validRoleAtSlot(role, slot) {
		e := ErrInvalidRole
		e.got = fmt.Sprintf("%v (%d) @ slot %v", role, role, slot)
		return dissemination, e
	}

	// Rule: exactly one signer.
	if len(signedSSVMessage.OperatorIDs) != 1 {
		return dissemination, ErrEnvelopeDisseminationMustHaveOneSigner
	}
	signer := signedSSVMessage.OperatorIDs[0]

	// Rule: full data is a consensus-only field.
	if len(signedSSVMessage.FullData) > 0 {
		return dissemination, ErrFullDataNotInConsensusMessage
	}

	state := mv.validatorState(ssvMessage.GetID(), committeeInfo)
	operatorState := state.OperatorState(committeeInfo.signerIndex(signer))

	// Rule: the slot must not regress once the signer advanced (the role is monotonic-slot).
	if maxSlot := operatorState.MaxSlot(); maxSlot != 0 && maxSlot > slot {
		e := ErrSlotAlreadyAdvanced
		e.got = slot
		e.want = maxSlot
		return dissemination, e
	}

	// Rule: the validator must hold the proposer assignment at the slot (shared with the role's
	// partial-signature rule, including its not-yet-fetched / stale-view tolerance).
	if err := mv.validateBeaconDuty(role, slot, committeeInfo.validatorIndices, false); err != nil {
		return dissemination, err
	}

	// Rule: one dissemination per (MessageID, signer, slot); a repeat is IGNORE'd regardless of content
	// or peer — an honest retry can repeat one after the gossip duplicate cache expires, so repetition
	// proves no fault. Another committee member's dissemination is admitted on its own budget.
	if signerState := operatorState.GetSignerStateForSlot(slot); signerState != nil && signerState.World.SeenEnvelopeDissemination {
		e := ErrDuplicatedEnvelopeDissemination
		e.got = fmt.Sprintf("slot %d, signer %d", slot, signer)
		return dissemination, e
	}

	// Rule: the short non-committee lateness window applies, with no earliness allowance.
	if err := mv.validateSlotTime(slot, role, receivedAt); err != nil {
		return dissemination, err
	}

	// Rule: at most SlotsPerEpoch envelope duties per epoch (one per proposal slot).
	if err := mv.validateDutyCount(ssvMessage.GetID(), slot, committeeInfo.validatorIndices, operatorState); err != nil {
		return dissemination, err
	}

	if err := ctx.Err(); err != nil {
		return dissemination, err
	}

	// Rule: the operator signature must verify before anything is recorded, otherwise a forged carrier
	// claiming another operator's identity would consume that operator's budget and suppress its
	// honest message.
	signature := signedSSVMessage.Signatures[0]
	if err := mv.signatureVerifier.VerifySignature(signer, ssvMessage, signature); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", signer, err)
		return dissemination, e
	}

	mv.recordEnvelopeDissemination(slot, operatorState)

	return dissemination, nil
}

// recordEnvelopeDissemination records the signer's accepted dissemination for the slot, creating the
// slot's signer state (and counting the duty) when the dissemination is the duty's first message. Only
// the signer-wide record is kept: a repeat is IGNORE'd whichever peer relays it (see the dedup rule), so a
// per-peer record would have no reader.
func (mv *messageValidator) recordEnvelopeDissemination(slot phase0.Slot, operatorState *OperatorState) {
	signerState := operatorState.GetSignerStateForSlot(slot)
	if signerState == nil {
		signerState = newSignerState(slot, specqbft.FirstRound)
		operatorState.SetSignerStateForSlot(slot, mv.netCfg.EstimatedEpochAtSlot(slot), signerState)
	}
	signerState.World.SeenEnvelopeDissemination = true
}
