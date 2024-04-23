package msgvalidation

// consensus_validation.go contains methods for validating consensus messages

import (
	"bytes"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/alan/qbft"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	"github.com/bloxapp/ssv-spec/types"
	"golang.org/x/exp/slices"

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
		e := ErrMalformedMessage
		e.innerErr = err
		return nil, e
	}

	if mv.operatorDataStore != nil && mv.operatorDataStore.OperatorIDReady() {
		if mv.ownCommittee(committee) {
			mv.metrics.CommitteeMessage(spectypes.SSVConsensusMsgType, mv.isDecidedMessage(signedSSVMessage, consensusMessage))
		} else {
			mv.metrics.NonCommitteeMessage(spectypes.SSVConsensusMsgType, mv.isDecidedMessage(signedSSVMessage, consensusMessage))
		}
	}

	messageID := ssvMessage.GetID()

	msgSlot := phase0.Slot(consensusMessage.Height)
	msgRound := consensusMessage.Round

	mv.metrics.ConsensusMsgType(consensusMessage.MsgType, len(signedSSVMessage.GetOperatorIDs()))

	switch messageID.GetRoleType() {
	case spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		e := ErrUnexpectedConsensusMessage
		e.got = messageID.GetRoleType()
		return consensusMessage, e
	}

	for _, signature := range signedSSVMessage.GetSignature() {
		if err := mv.validateSignatureFormat(signature); err != nil {
			return consensusMessage, err
		}
	}

	if !mv.validQBFTMsgType(consensusMessage.MsgType) {
		return consensusMessage, ErrUnknownQBFTMessageType
	}

	if err := mv.validConsensusSigners(signedSSVMessage, consensusMessage, committee); err != nil {
		return consensusMessage, err
	}

	role := messageID.GetRoleType()

	if err := mv.validateSlotTime(msgSlot, role, receivedAt); err != nil {
		return consensusMessage, err
	}

	if maxRound := mv.maxRound(role); msgRound > maxRound {
		err := ErrRoundTooHigh
		err.got = fmt.Sprintf("%v (%v role)", msgRound, role)
		err.want = fmt.Sprintf("%v (%v role)", maxRound, role)
		return consensusMessage, err
	}

	slotStartTime := mv.netCfg.Beacon.GetSlotStartTime(msgSlot) /*.
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

	if msgRound < lowestAllowed || msgRound > highestAllowed {
		err := ErrEstimatedRoundTooFar
		err.got = fmt.Sprintf("%v (%v role)", msgRound, role)
		err.want = fmt.Sprintf("between %v and %v (%v role) / %v passed", lowestAllowed, highestAllowed, role, sinceSlotStart)
		return consensusMessage, err
	}

	if mv.hasFullData(signedSSVMessage, consensusMessage) {
		hashedFullData, err := specqbft.HashDataRoot(signedSSVMessage.FullData)
		if err != nil {
			return consensusMessage, fmt.Errorf("hash data root: %w", err)
		}

		if hashedFullData != consensusMessage.Root {
			return consensusMessage, ErrInvalidHash
		}
	}

	if err := mv.validateBeaconDuty(messageID.GetRoleType(), msgSlot, validatorIndices); err != nil {
		return consensusMessage, err
	}

	state := mv.consensusState(messageID)
	for _, signer := range signedSSVMessage.GetOperatorIDs() {
		if err := mv.validateSignerBehaviorConsensus(state, signer, committee, signedSSVMessage, consensusMessage); err != nil {
			return consensusMessage, fmt.Errorf("bad signer behavior: %w", err)
		}
	}

	if err := mv.validateJustifications(consensusMessage); err != nil {
		return consensusMessage, err
	}

	for i, signature := range signedSSVMessage.GetSignature() {
		if err := mv.verifySignature(ssvMessage, signedSSVMessage.GetOperatorIDs()[i], signature); err != nil {
			return consensusMessage, err
		}
	}

	for _, signer := range signedSSVMessage.GetOperatorIDs() {
		signerState := state.GetSignerState(signer)
		if signerState == nil {
			signerState = state.CreateSignerState(signer)
		}
		if msgSlot > signerState.Slot {
			newEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) > mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot)
			signerState.ResetSlot(msgSlot, msgRound, newEpoch)
		} else if msgSlot == signerState.Slot && msgRound > signerState.Round {
			signerState.ResetRound(msgRound)
		}

		if mv.hasFullData(signedSSVMessage, consensusMessage) && signerState.ProposalData == nil {
			signerState.ProposalData = signedSSVMessage.FullData
		}

		signerState.MessageCounts.RecordConsensusMessage(signedSSVMessage, consensusMessage)
	}

	return consensusMessage, nil
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

func (mv *messageValidator) validateSignerBehaviorConsensus(
	state *consensusState,
	signer spectypes.OperatorID,
	committee []spectypes.OperatorID,
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
) error {
	signerState := state.GetSignerState(signer)
	if signerState == nil {
		return nil
	}

	msgSlot := phase0.Slot(consensusMessage.Height)
	msgRound := consensusMessage.Round

	if msgSlot < signerState.Slot {
		// Signers aren't allowed to decrease their slot.
		// If they've sent a future message due to clock error,
		// this should be caught by the earlyMessage check.
		err := ErrSlotAlreadyAdvanced
		err.want = signerState.Slot
		err.got = msgSlot
		return err
	}

	if msgSlot == signerState.Slot && msgRound < signerState.Round {
		// Signers aren't allowed to decrease their round.
		// If they've sent a future message due to clock error,
		// they'd have to wait for the next slot/round to be accepted.
		err := ErrRoundAlreadyAdvanced
		err.want = signerState.Round
		err.got = msgRound
		return err
	}

	newDutyInSameEpoch := false
	if msgSlot > signerState.Slot && mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) == mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot) {
		newDutyInSameEpoch = true
	}

	if err := mv.validateDutyCount(signerState, signedSSVMessage.SSVMessage.GetID(), newDutyInSameEpoch); err != nil {
		return err
	}

	if msgSlot == signerState.Slot && msgRound == signerState.Round {
		if mv.hasFullData(signedSSVMessage, consensusMessage) && signerState.ProposalData != nil && !bytes.Equal(signerState.ProposalData, signedSSVMessage.FullData) {
			return ErrDuplicatedProposalWithDifferentData
		}

		limits := maxMessageCounts(len(committee))
		if err := signerState.MessageCounts.ValidateConsensusMessage(signedSSVMessage, consensusMessage, limits); err != nil {
			return err
		}
	}

	return nil
}

func (mv *messageValidator) validateDutyCount(
	state *SignerState,
	msgID spectypes.MessageID,
	newDutyInSameEpoch bool,
) error {
	switch msgID.GetRoleType() {
	case spectypes.RoleAggregator, spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		limit := maxDutiesPerEpoch

		if sameSlot := !newDutyInSameEpoch; sameSlot {
			limit++
		}

		if state.EpochDuties >= limit {
			err := ErrTooManyDutiesPerEpoch
			err.got = fmt.Sprintf("%v (role %v)", state.EpochDuties, msgID.GetRoleType())
			err.want = fmt.Sprintf("less than %v", maxDutiesPerEpoch)
			return err
		}

		return nil
	default:
		return nil
	}
}

func (mv *messageValidator) validateBeaconDuty(
	role spectypes.RunnerRole,
	slot phase0.Slot,
	indices []phase0.ValidatorIndex,
) error {
	switch role {
	case spectypes.RoleProposer:
		epoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(slot)
		if mv.dutyStore != nil && mv.dutyStore.Proposer.ValidatorDuty(epoch, slot, indices[0]) == nil {
			return ErrNoDuty
		}

		return nil

	case spectypes.RoleSyncCommitteeContribution:
		period := mv.netCfg.Beacon.EstimatedSyncCommitteePeriodAtEpoch(mv.netCfg.Beacon.EstimatedEpochAtSlot(slot))
		if mv.dutyStore != nil && mv.dutyStore.SyncCommittee.Duty(period, indices[0]) == nil {
			return ErrNoDuty
		}

		return nil

	default:
		return nil
	}
}

func (mv *messageValidator) hasFullData(signedSSVMessage *spectypes.SignedSSVMessage, message *specqbft.Message) bool {
	return (message.MsgType == specqbft.ProposalMsgType ||
		message.MsgType == specqbft.RoundChangeMsgType ||
		mv.isDecidedMessage(signedSSVMessage, message)) && len(signedSSVMessage.FullData) != 0
}

func (mv *messageValidator) isDecidedMessage(signedSSVMessage *spectypes.SignedSSVMessage, message *specqbft.Message) bool {
	return message.MsgType == specqbft.CommitMsgType && len(signedSSVMessage.GetOperatorIDs()) > 1
}

func (mv *messageValidator) maxRound(role spectypes.RunnerRole) specqbft.Round {
	switch role {
	case spectypes.RoleCommittee, spectypes.RoleAggregator: // TODO: check if value for aggregator is correct as there are messages on stage exceeding the limit
		return 12 // TODO: consider calculating based on quick timeout and slow timeout
	case spectypes.RoleProposer, spectypes.RoleSyncCommitteeContribution:
		return 6
	case spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		return 0
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
	case spectypes.RoleProposer, spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		return 0
	default:
		panic("unknown role")
	}
}

func (mv *messageValidator) validQBFTMsgType(msgType specqbft.MessageType) bool {
	switch msgType {
	case specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType, specqbft.RoundChangeMsgType:
		return true
	default:
		return false
	}
}

func (mv *messageValidator) validConsensusSigners(signedMessage *spectypes.SignedSSVMessage, consensusMessage *specqbft.Message, committee []spectypes.OperatorID) error {
	signerCount := len(signedMessage.GetOperatorIDs())
	quorumSize, _ := ssvtypes.ComputeQuorumAndPartialQuorum(len(committee))

	switch {
	case signerCount == 0:
		return ErrNoSigners

	case signerCount == 1:
		if consensusMessage.MsgType == specqbft.ProposalMsgType {
			leader := mv.roundRobinProposer(consensusMessage.Height, consensusMessage.Round, committee)
			if signedMessage.GetOperatorIDs()[0] != leader {
				err := ErrSignerNotLeader
				err.got = signedMessage.GetOperatorIDs()[0]
				err.want = leader
				return err
			}
		}

	case consensusMessage.MsgType != specqbft.CommitMsgType:
		e := ErrNonDecidedWithMultipleSigners
		e.got = signerCount
		return e

	case uint64(signerCount) < quorumSize || signerCount > len(committee):
		e := ErrWrongSignersLength
		e.want = fmt.Sprintf("between %v and %v", quorumSize, len(committee))
		e.got = len(signedMessage.GetOperatorIDs())
		return e
	}

	if !slices.IsSorted(signedMessage.GetOperatorIDs()) {
		return ErrSignersNotSorted
	}

	var prevSigner spectypes.OperatorID
	for _, signer := range signedMessage.GetOperatorIDs() {
		if err := mv.commonSignerValidation(signer, committee); err != nil {
			return err
		}
		if signer == prevSigner {
			return ErrDuplicatedSigner
		}
		prevSigner = signer
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
