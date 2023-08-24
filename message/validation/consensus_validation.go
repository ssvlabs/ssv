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

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
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

	if err := mv.validConsensusSigners(share, signedMsg); err != nil {
		return err
	}

	messageSlot := phase0.Slot(signedMsg.Message.Height)

	role := msg.GetID().GetRoleType()
	if err := mv.validateSlotTime(messageSlot, role, receivedAt); err != nil {
		return err
	}

	msgRound := signedMsg.Message.Round
	maxRound := mv.maxRound(role)
	if msgRound > maxRound {
		err := ErrRoundTooHigh
		err.got = fmt.Sprintf("%v (%v role)", msgRound, role)
		err.want = fmt.Sprintf("%v (%v role)", maxRound, role)
		return err
	}

	// TODO: think about correct implementation of estimated round checks and then enable it
	//slotStartTime := mv.netCfg.Beacon.GetSlotStartTime(messageSlot).
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

	consensusID := ConsensusID{
		PubKey: phase0.BLSPubKey(msg.GetID().GetPubKey()),
		Role:   role,
	}

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

	msgSlot := phase0.Slot(signedMsg.Message.Height)

	for _, signer := range signedMsg.Signers {
		signerState := state.SignerState(signer)
		if msgSlot > signerState.Slot || msgSlot == signerState.Slot && msgRound > signerState.Round {
			signerState.Reset(msgSlot, msgRound)
		}

		if mv.hasFullData(signedMsg) && signerState.ProposalData == nil {
			signerState.ProposalData = signedMsg.FullData
		}

		state.SignerState(signer).MessageCounts.Record(msg)
	}

	return nil
}

func (mv *MessageValidator) validateJustifications(
	share *ssvtypes.SSVShare,
	signedMsg *specqbft.SignedMessage,
	height specqbft.Height,
	round specqbft.Round,
) error {
	pj, err := signedMsg.Message.GetPrepareJustifications()
	if err != nil {
		e := ErrMalformedPrepareJustifications
		e.innerErr = err
		return e
	}

	if len(pj) != 0 && signedMsg.Message.MsgType != specqbft.ProposalMsgType {
		e := ErrUnexpectedPrepareJustifications
		e.got = signedMsg.Message.MsgType
		return e
	}

	rcj, err := signedMsg.Message.GetRoundChangeJustifications()
	if err != nil {
		e := ErrMalformedRoundChangeJustifications
		e.innerErr = err
		return e
	}

	if len(rcj) != 0 && signedMsg.Message.MsgType != specqbft.ProposalMsgType && signedMsg.Message.MsgType != specqbft.RoundChangeMsgType {
		e := ErrUnexpectedRoundChangeJustifications
		e.got = signedMsg.Message.MsgType
		return e
	}

	if signedMsg.Message.MsgType == specqbft.ProposalMsgType {
		if err := instance.IsProposalJustification(share, rcj, pj, height, round, signedMsg.FullData); err != nil {
			e := ErrInvalidJustifications
			e.innerErr = err
			return e
		}
	}

	// TODO: other checks

	return nil
}

func (mv *MessageValidator) validateSignerBehavior(
	state *ConsensusState,
	signer spectypes.OperatorID,
	share *ssvtypes.SSVShare,
	msg *queue.DecodedSSVMessage,
) error {
	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		panic("validateSignerBehavior should be called on signed message")
	}

	signerState := state.SignerState(signer)

	msgSlot := phase0.Slot(signedMsg.Message.Height)
	msgRound := signedMsg.Message.Round

	if msgSlot < signerState.Slot {
		// Signers aren't allowed to decrease their slot.
		// If they've sent a future message due to clock error,
		// this should be caught by the earlyMessage check.
		err := ErrSlotAlreadyAdvanced
		err.want = signerState.Slot
		err.got = msgSlot
		return err
	}

	if msgRound < signerState.Round {
		// Signers aren't allowed to decrease their round.
		// If they've sent a future message due to clock error,
		// they'd have to wait for the next slot/round to be accepted.
		err := ErrRoundAlreadyAdvanced
		err.want = signerState.Round
		err.got = msgRound
		return err
	}

	if !(msgSlot > signerState.Slot || msgSlot == signerState.Slot && msgRound > signerState.Round) {
		if mv.hasFullData(signedMsg) {
			if signerState.ProposalData != nil && !bytes.Equal(signerState.ProposalData, signedMsg.FullData) {
				return ErrDuplicatedProposalWithDifferentData
			}
		}

		limits := maxMessageCounts(len(share.Committee), int(share.Quorum))
		if err := signerState.MessageCounts.Validate(msg, limits); err != nil {
			//return err
		}
	}

	if err := mv.validateJustifications(share, signedMsg, specqbft.Height(signerState.Slot), signerState.Round); err != nil {
		//return err
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
