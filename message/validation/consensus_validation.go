package validation

// consensus_validation.go contains methods for validating consensus messages

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func (mv *MessageValidator) validateConsensusMessage(share *ssvtypes.SSVShare, msg *queue.DecodedSSVMessage, receivedAt time.Time) error {
	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		return fmt.Errorf("expected consensus message")
	}

	if len(msg.Data) > maxConsensusMsgSize {
		return fmt.Errorf("size exceeded")
	}

	if err := mv.validateSignatureFormat(signedMsg.Signature); err != nil {
		return err
	}

	// TODO: check if has running duty

	if err := mv.validConsensusSigners(share, signedMsg); err != nil {
		return err
	}

	msgSlot := phase0.Slot(signedMsg.Message.Height)
	msgRound := signedMsg.Message.Round

	role := msg.GetID().GetRoleType()
	if err := mv.validateSlotTime(msgSlot, role, receivedAt); err != nil {
		return err
	}

	maxRound := mv.maxRound(role)
	if msgRound > maxRound {
		err := ErrRoundTooHigh
		err.got = fmt.Sprintf("%v (%v role)", msgRound, role)
		err.want = fmt.Sprintf("%v (%v role)", maxRound, role)
		return err
	}

	// TODO: think about correct implementation of estimated round checks and then enable it
	//slotStartTime := mv.netCfg.Beacon.GetSlotStartTime(msgSlot).
	//	Add(mv.waitAfterSlotStart(role)) // TODO: do we need this?
	//
	//sinceSlotStart := time.Duration(0)
	//estimatedRound := specqbft.FirstRound
	//if receivedAt.After(slotStartTime) {
	//	sinceSlotStart = receivedAt.Sub(slotStartTime)
	//	estimatedRound = mv.currentEstimatedRound(sinceSlotStart)
	//}
	//
	//lowestAllowed := estimatedRound - allowedRoundsInPast
	//highestAllowed := estimatedRound + allowedRoundsInFuture
	//
	//if msgRound < lowestAllowed || msgRound > highestAllowed {
	//	err := ErrEstimatedRoundTooFar
	//	err.got = fmt.Sprintf("%v (%v role)", msgRound, role)
	//	err.want = fmt.Sprintf("between %v and %v (%v role) / %v passed", lowestAllowed, highestAllowed, role, sinceSlotStart)
	//	return err
	//}

	if mv.hasFullData(signedMsg) {
		hashedFullData, err := specqbft.HashDataRoot(signedMsg.FullData)
		if err != nil {
			return fmt.Errorf("hash data root: %w", err)
		}

		if hashedFullData != signedMsg.Message.Root {
			return ErrInvalidHash
		}
	}

	pj, err := signedMsg.Message.GetPrepareJustifications()
	if err != nil {
		return fmt.Errorf("malfrormed prepare justifications: %w", err)
	}

	rcj, err := signedMsg.Message.GetRoundChangeJustifications()
	if err != nil {
		return fmt.Errorf("malfrormed round change justifications: %w", err)
	}

	// TODO: checks for pj and rcj (reject if wrong)
	_ = pj
	_ = rcj

	consensusID := ConsensusID{
		PubKey: phase0.BLSPubKey(msg.GetID().GetPubKey()),
		Role:   role,
	}
	// Validate each signer's behavior.
	state := mv.consensusState(consensusID)
	for _, signer := range signedMsg.Signers {
		if err := mv.validateSignerBehavior(state, signer, share, msg); err != nil {
			return fmt.Errorf("bad signer behavior: %w", err)
		}
	}

	if err := ssvtypes.VerifyByOperators(signedMsg.Signature, signedMsg, mv.netCfg.Domain, spectypes.QBFTSignatureType, share.Committee); err != nil {
		signErr := ErrInvalidSignature
		signErr.innerErr = err
		signErr.got = fmt.Sprintf("domain %v from %v", hex.EncodeToString(mv.netCfg.Domain[:]), hex.EncodeToString(share.ValidatorPubKey))
		return signErr
	}

	for _, signer := range signedMsg.Signers {
		signerState := state.SignerState(signer)
		if msgSlot > signerState.Slot {
			newEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) > mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot)
			signerState.ResetSlot(msgSlot, msgRound, newEpoch)
		} else if msgRound > signerState.Round {
			signerState.ResetRound(msgRound)
		}

		signerState.MessageCounts.Record(msg)
	}

	return nil
}

func (mv *MessageValidator) validateSignerBehavior(state *ConsensusState, signer spectypes.OperatorID, share *ssvtypes.SSVShare, msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		panic("validateSignerBehavior should be called on signed message")
	}

	signerState := state.SignerState(signer)

	if err := mv.validateDutiesCount(signerState, msg.MsgID.GetRoleType()); err != nil {
		return err
	}

	// TODO: make sure it's correct
	//if signerState.Slot < msgSlot && signerState.Round >= msgRound || signerState.Round < msgRound && signerState.Slot >= msgSlot {
	//return ErrFutureSlotRoundMismatch
	//}

	msgSlot := phase0.Slot(signedMsg.Message.Height)
	if err := mv.validateSlotState(signerState, msgSlot); err != nil {
		return err
	}

	if err := mv.validateRoundState(signerState, signedMsg.Message.Round); err != nil {
		return err
	}

	// TODO: if this is a round change message, we should somehow validate that it's not being sent too frequently.

	// TODO: move to MessageCounts?
	if mv.hasFullData(signedMsg) {
		if signerState.ProposalData == nil {
			signerState.ProposalData = signedMsg.FullData
		} else if !bytes.Equal(signerState.ProposalData, signedMsg.FullData) {
			return ErrDuplicatedProposalWithDifferentData
		}
	}

	limits := maxMessageCounts(len(share.Committee), int(share.Quorum))
	if err := signerState.MessageCounts.Validate(msg, limits); err != nil {
		return err
	}

	return nil
}

func (mv *MessageValidator) validateDutiesCount(state *SignerState, role spectypes.BeaconRole) error {
	switch role {
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator, spectypes.BNRoleValidatorRegistration:
		if state.EpochDuties > maxDutiesPerEpoch {
			err := ErrTooManyDutiesPerEpoch
			err.got = fmt.Sprintf("%v (role %v)", state.EpochDuties, role)
			err.want = maxDutiesPerEpoch
			return err
		}
	}

	return nil
}

func (mv *MessageValidator) hasFullData(signedMsg *specqbft.SignedMessage) bool {
	return (signedMsg.Message.MsgType == specqbft.ProposalMsgType ||
		signedMsg.Message.MsgType == specqbft.RoundChangeMsgType ||
		mv.isDecidedMessage(signedMsg)) && len(signedMsg.FullData) != 0 // TODO: more complex check of FullData
}

func (mv *MessageValidator) isDecidedMessage(signedMsg *specqbft.SignedMessage) bool {
	return signedMsg.Message.MsgType == specqbft.CommitMsgType && len(signedMsg.Signers) > 1
}

func (mv *MessageValidator) maxRound(role spectypes.BeaconRole) specqbft.Round {
	switch role {
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator: // TODO: check if value for aggregator is correct as there are messages on stage exceeding the limit
		return 12 // TODO: consider calculating based on quick timeout and slow timeout
	case spectypes.BNRoleProposer, spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		return 6
	case spectypes.BNRoleValidatorRegistration:
		return 0
	default:
		panic("unknown role")
	}
}

func (mv *MessageValidator) currentEstimatedRound(sinceSlotStart time.Duration) specqbft.Round {
	if currentQuickRound := specqbft.FirstRound + specqbft.Round(sinceSlotStart/roundtimer.QuickTimeout); currentQuickRound <= roundtimer.QuickTimeoutThreshold {
		return currentQuickRound
	}

	sinceFirstSlowRound := sinceSlotStart - (time.Duration(roundtimer.QuickTimeoutThreshold) * roundtimer.QuickTimeout)
	estimatedRound := roundtimer.QuickTimeoutThreshold + specqbft.FirstRound + specqbft.Round(sinceFirstSlowRound/roundtimer.SlowTimeout)
	return estimatedRound
}

func (mv *MessageValidator) waitAfterSlotStart(role spectypes.BeaconRole) time.Duration {
	switch role {
	case spectypes.BNRoleSyncCommitteeContribution:
		return mv.netCfg.Beacon.SlotDurationSec() // TODO: do we need this?
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator, spectypes.BNRoleSyncCommittee, spectypes.BNRoleProposer, spectypes.BNRoleValidatorRegistration:
		return 0
	default:
		panic("unknown role")
	}
}

func (mv *MessageValidator) validRole(roleType spectypes.BeaconRole) bool {
	switch roleType {
	case spectypes.BNRoleAttester,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleProposer,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
		spectypes.BNRoleValidatorRegistration:
		return true
	}
	return false
}

func (mv *MessageValidator) validConsensusSigners(share *ssvtypes.SSVShare, m *specqbft.SignedMessage) error {
	if len(m.Signers) == 0 {
		return ErrNoSigners
	}

	if len(m.Signers) == 1 {
		if m.Message.MsgType == specqbft.ProposalMsgType {
			qbftState := &specqbft.State{
				Height: m.Message.Height,
				Share:  &share.Share,
			}
			leader := specqbft.RoundRobinProposer(qbftState, m.Message.Round)
			if m.Signers[0] != leader {
				err := ErrSignerNotLeader
				err.got = m.Signers[0]
				err.want = leader
				return err
			}
		}
	} else if m.Message.MsgType != specqbft.CommitMsgType {
		e := ErrNonDecidedWithMultipleSigners
		e.got = len(m.Signers)
		return e
	} else if uint64(len(m.Signers)) < share.Quorum || len(m.Signers) > len(share.Committee) {
		e := ErrWrongSignersLength
		e.want = fmt.Sprintf("between %v and %v", share.Quorum, share.Committee)
		e.got = len(m.Signers)
		return e
	}

	if !slices.IsSorted(m.Signers) {
		return ErrSignersNotSorted
	}

	seen := map[spectypes.OperatorID]struct{}{}
	for _, signer := range m.Signers {
		if err := mv.commonSignerValidation(signer, share); err != nil {
			return err
		}

		if _, ok := seen[signer]; ok {
			return ErrDuplicatedSigner
		}
		seen[signer] = struct{}{}
	}
	return nil
}
