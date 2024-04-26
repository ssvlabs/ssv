package msgvalidation

// consensus_validation.go contains methods for validating consensus messages

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/alan/qbft"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	"github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func (mv *messageValidator) validateConsensusMessage(
	signedSSVMessage *spectypes.SignedSSVMessage,
	committee []spectypes.OperatorID,
	validatorIndices []phase0.ValidatorIndex,
	receivedAt time.Time,
) (*specqbft.Message, error) {
	ssvMessage := signedSSVMessage.GetSSVMessage()

	if len(ssvMessage.Data) > maxConsensusMsgSize {
		e := ErrSSVDataTooBig
		e.got = len(ssvMessage.Data)
		e.want = maxConsensusMsgSize
		return nil, e
	}

	consensusMessage, err := specqbft.DecodeMessage(ssvMessage.Data)
	if err != nil {
		e := ErrUndecodableMessageData
		e.innerErr = err
		return nil, e
	}

	mv.reportConsensusMessageMetrics(signedSSVMessage, consensusMessage, committee)

	if err := mv.validateConsensusMessageSemantics(signedSSVMessage, consensusMessage, committee); err != nil {
		return consensusMessage, err
	}

	state := mv.consensusState(signedSSVMessage.GetSSVMessage().GetID())

	if err := mv.validateQBFTLogic(signedSSVMessage, consensusMessage, committee, receivedAt, state); err != nil {
		return consensusMessage, err
	}

	if err := mv.validateQBFTMessageByDutyLogic(signedSSVMessage, consensusMessage, validatorIndices, receivedAt, state); err != nil {
		return consensusMessage, err
	}

	for i := range signedSSVMessage.GetSignature() {
		operatorID := signedSSVMessage.GetOperatorIDs()[i]
		signature := signedSSVMessage.GetSignature()[i]

		if err := mv.signatureVerifier.VerifySignature(operatorID, ssvMessage, signature); err != nil {
			e := ErrSignatureVerification
			e.innerErr = fmt.Errorf("verify opid: %v signature: %w", operatorID, err)
			return consensusMessage, e
		}
	}

	mv.updateConsensusState(signedSSVMessage, consensusMessage, state)

	return consensusMessage, nil
}

func (mv *messageValidator) validateConsensusMessageSemantics(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	committee []spectypes.OperatorID,
) error {
	signers := signedSSVMessage.GetOperatorIDs()
	quorumSize, _ := ssvtypes.ComputeQuorumAndPartialQuorum(len(committee))
	if len(signers) > 1 {
		if consensusMessage.MsgType != specqbft.CommitMsgType {
			e := ErrNonDecidedWithMultipleSigners
			e.got = len(signers)
			return e
		}

		if uint64(len(signers)) < quorumSize {
			e := ErrDecidedNotEnoughSigners
			e.want = quorumSize
			e.got = len(signers)
			return e
		}
	}

	if len(signedSSVMessage.FullData) != 0 {
		if consensusMessage.MsgType == specqbft.PrepareMsgType ||
			consensusMessage.MsgType == specqbft.CommitMsgType && len(signedSSVMessage.GetOperatorIDs()) == 1 {
			return ErrPrepareOrCommitWithFullData
		}

		hashedFullData, err := specqbft.HashDataRoot(signedSSVMessage.FullData)
		if err != nil {
			e := ErrFullDataHash
			e.innerErr = err
			return e
		}

		if hashedFullData != consensusMessage.Root {
			return ErrInvalidHash
		}
	}

	if !mv.validConsensusMsgType(consensusMessage.MsgType) {
		return ErrUnknownQBFTMessageType
	}

	if consensusMessage.Round == specqbft.NoRound {
		e := ErrInvalidRound
		e.got = specqbft.NoRound
		return e
	}

	if !bytes.Equal(consensusMessage.Identifier, signedSSVMessage.GetSSVMessage().MsgID[:]) {
		e := ErrMismatchedIdentifier
		e.want = hex.EncodeToString(signedSSVMessage.GetSSVMessage().MsgID[:])
		e.got = hex.EncodeToString(consensusMessage.Identifier)
		return e
	}

	if err := mv.validateJustifications(consensusMessage); err != nil {
		return err
	}

	return nil
}

func (mv *messageValidator) validateQBFTLogic(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	committee []spectypes.OperatorID,
	receivedAt time.Time,
	state *consensusState,
) error {
	if consensusMessage.MsgType == specqbft.ProposalMsgType {
		leader := mv.roundRobinProposer(consensusMessage.Height, consensusMessage.Round, committee)
		if signedSSVMessage.GetOperatorIDs()[0] != leader {
			err := ErrSignerNotLeader
			err.got = signedSSVMessage.GetOperatorIDs()[0]
			err.want = leader
			return err
		}
	}

	msgSlot := phase0.Slot(consensusMessage.Height)
	for _, signer := range signedSSVMessage.GetOperatorIDs() {
		signerState := state.GetSignerState(signer)
		if signerState == nil {
			continue
		}

		// It should be checked after ErrNonDecidedWithMultipleSigners
		signerCount := len(signedSSVMessage.GetOperatorIDs())
		if signerCount > 1 {
			if _, ok := signerState.SeenDecidedLengths[signerCount]; ok {
				return ErrDecidedWithSameNumberOfSigners
			}
		}

		if msgSlot == signerState.Slot && consensusMessage.Round == signerState.Round {
			if len(signedSSVMessage.FullData) != 0 && signerState.ProposalData != nil && !bytes.Equal(signerState.ProposalData, signedSSVMessage.FullData) {
				return ErrDuplicatedProposalWithDifferentData
			}

			limits := maxMessageCounts(len(committee))
			if err := signerState.MessageCounts.ValidateConsensusMessage(signedSSVMessage, consensusMessage, limits); err != nil {
				return err
			}
		}
	}

	return mv.roundBelongsToAllowedSpread(signedSSVMessage, consensusMessage, receivedAt)
}

func (mv *messageValidator) validateQBFTMessageByDutyLogic(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	validatorIndices []phase0.ValidatorIndex,
	receivedAt time.Time,
	state *consensusState,
) error {
	role := signedSSVMessage.GetSSVMessage().GetID().GetRoleType()
	if role == spectypes.RoleValidatorRegistration || role == spectypes.RoleVoluntaryExit {
		e := ErrUnexpectedConsensusMessage
		e.got = role
		return e
	}

	msgSlot := phase0.Slot(consensusMessage.Height)
	if err := mv.validateBeaconDuty(signedSSVMessage.GetSSVMessage().GetID().GetRoleType(), msgSlot, validatorIndices); err != nil {
		return err
	}

	if err := mv.validateSlotTime(msgSlot, role, receivedAt); err != nil {
		return err
	}

	for _, signer := range signedSSVMessage.GetOperatorIDs() {
		signerState := state.GetSignerState(signer)
		if signerState == nil {
			continue
		}

		if err := mv.validNumberOfCommitteeDutiesPerEpoch(signedSSVMessage, validatorIndices, signerState, msgSlot); err != nil {
			return err
		}
	}

	if maxRound := mv.maxRound(role); consensusMessage.Round > maxRound {
		err := ErrRoundTooHigh
		err.got = fmt.Sprintf("%v (%v role)", consensusMessage.Round, role)
		err.want = fmt.Sprintf("%v (%v role)", maxRound, role)
		return err
	}

	return nil
}

func (mv *messageValidator) updateConsensusState(signedSSVMessage *spectypes.SignedSSVMessage, consensusMessage *specqbft.Message, state *consensusState) {
	msgSlot := phase0.Slot(consensusMessage.Height)

	for _, signer := range signedSSVMessage.GetOperatorIDs() {
		signerState := state.GetSignerState(signer)
		if signerState == nil {
			signerState = state.CreateSignerState(signer)
		}
		if msgSlot > signerState.Slot {
			newEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) > mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot)
			signerState.ResetSlot(msgSlot, consensusMessage.Round, newEpoch)
		} else if msgSlot == signerState.Slot && consensusMessage.Round > signerState.Round {
			signerState.ResetRound(consensusMessage.Round)
		}

		if len(signedSSVMessage.FullData) != 0 && signerState.ProposalData == nil {
			signerState.ProposalData = signedSSVMessage.FullData
		}

		signerCount := len(signedSSVMessage.GetOperatorIDs())
		if signerCount > 1 {
			signerState.SeenDecidedLengths[signerCount] = struct{}{}
		}

		signerState.MessageCounts.RecordConsensusMessage(signedSSVMessage, consensusMessage)
	}
}

func (mv *messageValidator) validNumberOfCommitteeDutiesPerEpoch(
	signedSSVMessage *spectypes.SignedSSVMessage,
	validatorIndices []phase0.ValidatorIndex,
	signerState *SignerState,
	msgSlot phase0.Slot,
) error {
	newDutyInSameEpoch := false
	if msgSlot > signerState.Slot && mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) == mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot) {
		newDutyInSameEpoch = true
	}

	if err := mv.validateDutyCount(validatorIndices, signerState, signedSSVMessage.SSVMessage.GetID(), newDutyInSameEpoch); err != nil {
		return err
	}

	return nil
}

func (mv *messageValidator) validateJustifications(message *specqbft.Message) error {
	pj, err := message.GetPrepareJustifications()
	if err != nil {
		e := ErrMalformedPrepareJustifications
		e.innerErr = err
		return e
	}

	if len(pj) != 0 && message.MsgType != specqbft.ProposalMsgType {
		e := ErrUnexpectedPrepareJustifications
		e.got = message.MsgType
		return e
	}

	rcj, err := message.GetRoundChangeJustifications()
	if err != nil {
		e := ErrMalformedRoundChangeJustifications
		e.innerErr = err
		return e
	}

	if len(rcj) != 0 && message.MsgType != specqbft.ProposalMsgType && message.MsgType != specqbft.RoundChangeMsgType {
		e := ErrUnexpectedRoundChangeJustifications
		e.got = message.MsgType
		return e
	}

	return nil
}

func (mv *messageValidator) validateBeaconDuty(
	role spectypes.RunnerRole,
	slot phase0.Slot,
	indices []phase0.ValidatorIndex,
) error {
	if role == spectypes.RoleProposer {
		epoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(slot)
		if mv.dutyStore != nil && mv.dutyStore.Proposer.ValidatorDuty(epoch, slot, indices[0]) == nil {
			return ErrNoDuty
		}
	}

	if role == spectypes.RoleSyncCommitteeContribution {
		period := mv.netCfg.Beacon.EstimatedSyncCommitteePeriodAtEpoch(mv.netCfg.Beacon.EstimatedEpochAtSlot(slot))
		if mv.dutyStore != nil && mv.dutyStore.SyncCommittee.Duty(period, indices[0]) == nil {
			return ErrNoDuty
		}
	}

	return nil
}

func (mv *messageValidator) maxRound(role spectypes.RunnerRole) specqbft.Round {
	switch role {
	case spectypes.RoleCommittee, spectypes.RoleAggregator: // TODO: check if value for aggregator is correct as there are messages on stage exceeding the limit
		return 12 // TODO: consider calculating based on quick timeout and slow timeout
	case spectypes.RoleProposer, spectypes.RoleSyncCommitteeContribution:
		return 6
	default:
		panic("unknown role")
	}
}

func (mv *messageValidator) currentEstimatedRound(sinceSlotStart time.Duration) specqbft.Round {
	if currentQuickRound := specqbft.FirstRound + specqbft.Round(sinceSlotStart/roundtimer.QuickTimeout); currentQuickRound <= specqbft.Round(roundtimer.QuickTimeoutThreshold) {
		return currentQuickRound
	}

	sinceFirstSlowRound := sinceSlotStart - (time.Duration(specqbft.Round(roundtimer.QuickTimeoutThreshold)) * roundtimer.QuickTimeout)
	estimatedRound := specqbft.Round(roundtimer.QuickTimeoutThreshold) + specqbft.FirstRound + specqbft.Round(sinceFirstSlowRound/roundtimer.SlowTimeout)
	return estimatedRound
}

func (mv *messageValidator) waitAfterSlotStart(role spectypes.RunnerRole) time.Duration {
	switch role {
	case spectypes.RoleCommittee:
		return mv.netCfg.Beacon.SlotDurationSec() / 3
	case spectypes.RoleAggregator, spectypes.RoleSyncCommitteeContribution:
		return mv.netCfg.Beacon.SlotDurationSec() / 3 * 2
	case spectypes.RoleProposer:
		return 0
	default:
		panic("unknown role")
	}
}

func (mv *messageValidator) validConsensusMsgType(msgType specqbft.MessageType) bool {
	switch msgType {
	case specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType, specqbft.RoundChangeMsgType:
		return true
	default:
		return false
	}
}

func (mv *messageValidator) roundBelongsToAllowedSpread(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	receivedAt time.Time,
) error {
	slotStartTime := mv.netCfg.Beacon.GetSlotStartTime(phase0.Slot(consensusMessage.Height)) /*.
	Add(mv.waitAfterSlotStart(role))*/ // TODO: not supported yet because first round is non-deterministic now

	sinceSlotStart := time.Duration(0)
	estimatedRound := specqbft.FirstRound
	if receivedAt.After(slotStartTime) {
		sinceSlotStart = receivedAt.Sub(slotStartTime)
		estimatedRound = mv.currentEstimatedRound(sinceSlotStart)
	}

	// TODO: lowestAllowed is not supported yet because first round is non-deterministic now
	lowestAllowed := /*estimatedRound - allowedRoundsInPast*/ specqbft.FirstRound
	highestAllowed := estimatedRound + allowedRoundsInFuture

	role := signedSSVMessage.GetSSVMessage().GetID().GetRoleType()

	if consensusMessage.Round < lowestAllowed || consensusMessage.Round > highestAllowed {
		e := ErrEstimatedRoundTooFar
		e.got = fmt.Sprintf("%v (%v role)", consensusMessage.Round, role)
		e.want = fmt.Sprintf("between %v and %v (%v role) / %v passed", lowestAllowed, highestAllowed, role, sinceSlotStart)
		return e
	}

	return nil
}

func (mv *messageValidator) roundRobinProposer(height specqbft.Height, round specqbft.Round, committee []spectypes.OperatorID) types.OperatorID {
	firstRoundIndex := 0
	if height != specqbft.FirstHeight {
		firstRoundIndex += int(height) % len(committee)
	}

	index := (firstRoundIndex + int(round) - int(specqbft.FirstRound)) % len(committee)
	return committee[index]
}

func (mv *messageValidator) reportConsensusMessageMetrics(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	committee []spectypes.OperatorID,
) {
	if mv.operatorDataStore != nil && mv.operatorDataStore.OperatorIDReady() {
		if mv.ownCommittee(committee) {
			mv.metrics.CommitteeMessage(spectypes.SSVConsensusMsgType, mv.isDecidedMessage(signedSSVMessage, consensusMessage))
		} else {
			mv.metrics.NonCommitteeMessage(spectypes.SSVConsensusMsgType, mv.isDecidedMessage(signedSSVMessage, consensusMessage))
		}
	}

	mv.metrics.ConsensusMsgType(consensusMessage.MsgType, len(signedSSVMessage.GetOperatorIDs()))
}

func (mv *messageValidator) isDecidedMessage(signedSSVMessage *spectypes.SignedSSVMessage, message *specqbft.Message) bool {
	return message.MsgType == specqbft.CommitMsgType && len(signedSSVMessage.GetOperatorIDs()) > 1
}
