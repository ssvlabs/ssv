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

func (mv *MessageValidator) validateConsensusSignature(share *ssvtypes.SSVShare, msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		return fmt.Errorf("expected consensus message")
	}

	if err := ssvtypes.VerifyByOperators(signedMsg.Signature, signedMsg, mv.netCfg.Domain, spectypes.QBFTSignatureType, share.Committee); err != nil {
		signErr := ErrInvalidSignature
		signErr.innerErr = err
		signErr.got = fmt.Sprintf("domain %v from %v", hex.EncodeToString(mv.netCfg.Domain[:]), hex.EncodeToString(share.ValidatorPubKey))
		return signErr
	}

	return nil
}

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

	slotStartTime := mv.netCfg.Beacon.GetSlotStartTime(messageSlot).
		Add(mv.waitAfterSlotStart(role)) // TODO: do we need this?

	if receivedAt.Before(slotStartTime) {
		err := ErrReceivedBeforeSlotStart
		err.want = slotStartTime.String()
		err.got = receivedAt.String()
		return err
	}

	sinceSlotStart := receivedAt.Sub(slotStartTime)
	estimatedRound := mv.currentEstimatedRound(sinceSlotStart)

	if estimatedRound > maxRound {
		err := ErrEstimatedRoundTooHigh
		err.got = fmt.Sprintf("%v (%v role) / %v passed", estimatedRound, role, sinceSlotStart)
		err.want = fmt.Sprintf("%v (%v role)", maxRound, role)
		return err
	}

	lowestAllowed, highestAllowed := estimatedRound-allowedRoundsInPast, estimatedRound+allowedRoundsInFuture
	if msgRound < lowestAllowed || msgRound > highestAllowed {
		// TODO: enable
		//err := ErrEstimatedRoundTooFar
		//err.got = fmt.Sprintf("%v (%v role)", msgRound, role)
		//err.want = fmt.Sprintf("between %v and %v (%v role) / %v passed", lowestAllowed, highestAllowed, role, sinceSlotStart)
		//return err
	}

	if mv.hasFullData(signedMsg) {
		hashedFullData, err := specqbft.HashDataRoot(signedMsg.FullData)
		if err != nil {
			return fmt.Errorf("hash data root: %w", err)
		}

		if hashedFullData != signedMsg.Message.Root {
			return fmt.Errorf("root doesn't match full data hash")
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

	// TODO: checks for pj and rcj
	_ = pj
	_ = rcj

	consensusID := ConsensusID{
		PubKey: phase0.BLSPubKey(msg.GetID().GetPubKey()),
		Role:   role,
	}
	// Validate each signer's behavior.
	state := mv.consensusState(consensusID)
	for _, signer := range signedMsg.Signers {
		if err := mv.validateSignerBehavior(state, signer, msg); err != nil {
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
		state.SignerState(signer).MessageCounts.Record(msg)
	}

	return nil
}

func (mv *MessageValidator) validateSignerBehavior(
	state *ConsensusState,
	signer spectypes.OperatorID,
	msg *queue.DecodedSSVMessage,
) error {
	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		// TODO: add support
		return fmt.Errorf("not supported yet")
	}

	signerState := state.SignerState(signer)

	err := mv.validateSlotAndRoundState(signerState, phase0.Slot(signedMsg.Message.Height), signedMsg.Message.Round)
	if err != nil {
		return err
	}

	// TODO: if this is a round change message, we should somehow validate that it's not being sent too frequently.

	// TODO: move to MessageCounts?
	if mv.hasFullData(signedMsg) {
		if signerState.ProposalData == nil {
			signerState.ProposalData = signedMsg.FullData
		} else if !bytes.Equal(signerState.ProposalData, signedMsg.FullData) {
			return fmt.Errorf("duplicated proposal with different data")
		}
	}

	if mv.isDecidedMessage(signedMsg) && len(signedMsg.Signers) <= signerState.LastDecidedQuorumSize {
		// TODO: make sure check is correct
		//return fmt.Errorf("decided must have more signers than previous decided")
	}

	signerState.LastDecidedQuorumSize = len(signedMsg.Signers)

	if err := signerState.MessageCounts.Validate(msg); err != nil {
		// TODO: make sure check is correct
		//return err
	}

	// Validate message counts within the current round.
	if signerState.MessageCounts.ReachedLimits(maxMessageCounts(len(signedMsg.Signers))) {
		// TODO: make sure check is correct
		//return ErrTooManyMessagesPerRound
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
	case spectypes.BNRoleAttester, spectypes.BNRoleSyncCommittee:
		return mv.netCfg.Beacon.SlotDurationSec() / 3
	case spectypes.BNRoleAggregator, spectypes.BNRoleSyncCommitteeContribution:
		return mv.netCfg.Beacon.SlotDurationSec() / 3 * 2
	case spectypes.BNRoleProposer, spectypes.BNRoleValidatorRegistration:
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
		return fmt.Errorf("non-decided with multiple signers, len: %d", len(m.Signers))
	} else if uint64(len(m.Signers)) < share.Quorum || len(m.Signers) > len(share.Committee) {
		return fmt.Errorf("decided signers size is not between partial quorum and quorum size")
	}

	if !slices.IsSorted(m.Signers) {
		return ErrSignersNotSorted
	}

	// TODO: if decided, check if previous decided had less or equal signers

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
