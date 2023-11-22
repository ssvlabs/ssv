package validation

// consensus_validation.go contains methods for validating consensus messages

import (
	"bytes"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func (mv *messageValidator) validateConsensusMessage(
	share *ssvtypes.SSVShare,
	signedMsg *specqbft.SignedMessage,
	messageID spectypes.MessageID,
	receivedAt time.Time,
	signatureVerifier func() error,
) (ConsensusDescriptor, phase0.Slot, error) {
	var consensusDescriptor ConsensusDescriptor

	if mv.inCommittee(share) {
		mv.metrics.InCommitteeMessage(spectypes.SSVConsensusMsgType, mv.isDecidedMessage(signedMsg))
	} else {
		mv.metrics.NonCommitteeMessage(spectypes.SSVConsensusMsgType, mv.isDecidedMessage(signedMsg))
	}

	msgSlot := phase0.Slot(signedMsg.Message.Height)
	msgRound := signedMsg.Message.Round

	consensusDescriptor = ConsensusDescriptor{
		QBFTMessageType: signedMsg.Message.MsgType,
		Round:           msgRound,
		Signers:         signedMsg.Signers,
		Committee:       share.Committee,
	}

	mv.metrics.ConsensusMsgType(signedMsg.Message.MsgType, len(signedMsg.Signers))

	if messageID.GetRoleType() == spectypes.BNRoleValidatorRegistration {
		return consensusDescriptor, msgSlot, ErrConsensusValidatorRegistration
	}

	if err := mv.validateSignatureFormat(signedMsg.Signature); err != nil {
		return consensusDescriptor, msgSlot, err
	}

	if !mv.validQBFTMsgType(signedMsg.Message.MsgType) {
		return consensusDescriptor, msgSlot, ErrUnknownQBFTMessageType
	}

	if err := mv.validConsensusSigners(share, signedMsg); err != nil {
		return consensusDescriptor, msgSlot, err
	}

	role := messageID.GetRoleType()

	if err := mv.validateSlotTime(msgSlot, role, receivedAt); err != nil {
		return consensusDescriptor, msgSlot, err
	}

	if maxRound := mv.maxRound(role); msgRound > maxRound {
		err := ErrRoundTooHigh
		err.got = fmt.Sprintf("%v (%v role)", msgRound, role)
		err.want = fmt.Sprintf("%v (%v role)", maxRound, role)
		return consensusDescriptor, msgSlot, err
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
		return consensusDescriptor, msgSlot, err
	}

	if mv.hasFullData(signedMsg) {
		hashedFullData, err := specqbft.HashDataRoot(signedMsg.FullData)
		if err != nil {
			return consensusDescriptor, msgSlot, fmt.Errorf("hash data root: %w", err)
		}

		if hashedFullData != signedMsg.Message.Root {
			return consensusDescriptor, msgSlot, ErrInvalidHash
		}
	}

	if err := mv.validateBeaconDuty(messageID.GetRoleType(), msgSlot, share); err != nil {
		return consensusDescriptor, msgSlot, err
	}

	state := mv.consensusState(messageID)
	for _, signer := range signedMsg.Signers {
		if err := mv.validateSignerBehaviorConsensus(state, signer, share, messageID, signedMsg); err != nil {
			return consensusDescriptor, msgSlot, fmt.Errorf("bad signer behavior: %w", err)
		}
	}

	if signatureVerifier != nil {
		if err := signatureVerifier(); err != nil {
			return consensusDescriptor, msgSlot, err
		}
	}

	for _, signer := range signedMsg.Signers {
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

		if mv.hasFullData(signedMsg) && signerState.ProposalData == nil {
			signerState.ProposalData = signedMsg.FullData
		}

		signerState.MessageCounts.RecordConsensusMessage(signedMsg)
	}

	return consensusDescriptor, msgSlot, nil
}

func (mv *messageValidator) validateJustifications(
	share *ssvtypes.SSVShare,
	signedMsg *specqbft.SignedMessage,
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
		cfg := newQBFTConfig(mv.netCfg.Domain)

		if err := instance.IsProposalJustification(
			cfg,
			share,
			rcj,
			pj,
			signedMsg.Message.Height,
			signedMsg.Message.Round,
			signedMsg.FullData,
		); err != nil {
			e := ErrInvalidJustifications
			e.innerErr = err
			return e
		}
	}

	return nil
}

func (mv *messageValidator) validateSignerBehaviorConsensus(
	state *ConsensusState,
	signer spectypes.OperatorID,
	share *ssvtypes.SSVShare,
	msgID spectypes.MessageID,
	signedMsg *specqbft.SignedMessage,
) error {
	signerState := state.GetSignerState(signer)

	if signerState == nil {
		return mv.validateJustifications(share, signedMsg)
	}

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

	if err := mv.validateDutyCount(signerState, msgID, newDutyInSameEpoch); err != nil {
		return err
	}

	if msgSlot == signerState.Slot && msgRound == signerState.Round {
		if mv.hasFullData(signedMsg) && signerState.ProposalData != nil && !bytes.Equal(signerState.ProposalData, signedMsg.FullData) {
			return ErrDuplicatedProposalWithDifferentData
		}

		limits := maxMessageCounts(len(share.Committee))
		if err := signerState.MessageCounts.ValidateConsensusMessage(signedMsg, limits); err != nil {
			return err
		}
	}

	return mv.validateJustifications(share, signedMsg)
}

func (mv *messageValidator) validateDutyCount(
	state *SignerState,
	msgID spectypes.MessageID,
	newDutyInSameEpoch bool,
) error {
	switch msgID.GetRoleType() {
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator, spectypes.BNRoleValidatorRegistration:
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
	}

	return nil
}

func (mv *messageValidator) validateBeaconDuty(
	role spectypes.BeaconRole,
	slot phase0.Slot,
	share *ssvtypes.SSVShare,
) error {
	switch role {
	case spectypes.BNRoleProposer:
		if share.Metadata.BeaconMetadata == nil {
			return ErrNoShareMetadata
		}

		epoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(slot)
		if mv.dutyStore != nil && mv.dutyStore.Proposer.ValidatorDuty(epoch, slot, share.Metadata.BeaconMetadata.Index) == nil {
			return ErrNoDuty
		}

		return nil

	case spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		if share.Metadata.BeaconMetadata == nil {
			return ErrNoShareMetadata
		}

		period := mv.netCfg.Beacon.EstimatedSyncCommitteePeriodAtEpoch(mv.netCfg.Beacon.EstimatedEpochAtSlot(slot))
		if mv.dutyStore != nil && mv.dutyStore.SyncCommittee.Duty(period, share.Metadata.BeaconMetadata.Index) == nil {
			return ErrNoDuty
		}

		return nil
	}

	return nil
}

func (mv *messageValidator) hasFullData(signedMsg *specqbft.SignedMessage) bool {
	return (signedMsg.Message.MsgType == specqbft.ProposalMsgType ||
		signedMsg.Message.MsgType == specqbft.RoundChangeMsgType ||
		mv.isDecidedMessage(signedMsg)) && len(signedMsg.FullData) != 0 // TODO: more complex check of FullData
}

func (mv *messageValidator) isDecidedMessage(signedMsg *specqbft.SignedMessage) bool {
	return signedMsg.Message.MsgType == specqbft.CommitMsgType && len(signedMsg.Signers) > 1
}

func (mv *messageValidator) maxRound(role spectypes.BeaconRole) specqbft.Round {
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

func (mv *messageValidator) currentEstimatedRound(sinceSlotStart time.Duration) specqbft.Round {
	if currentQuickRound := specqbft.FirstRound + specqbft.Round(sinceSlotStart/roundtimer.QuickTimeout); currentQuickRound <= roundtimer.QuickTimeoutThreshold {
		return currentQuickRound
	}

	sinceFirstSlowRound := sinceSlotStart - (time.Duration(roundtimer.QuickTimeoutThreshold) * roundtimer.QuickTimeout)
	estimatedRound := roundtimer.QuickTimeoutThreshold + specqbft.FirstRound + specqbft.Round(sinceFirstSlowRound/roundtimer.SlowTimeout)
	return estimatedRound
}

func (mv *messageValidator) waitAfterSlotStart(role spectypes.BeaconRole) time.Duration {
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

func (mv *messageValidator) validRole(roleType spectypes.BeaconRole) bool {
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

func (mv *messageValidator) validQBFTMsgType(msgType specqbft.MessageType) bool {
	switch msgType {
	case specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType, specqbft.RoundChangeMsgType:
		return true
	}
	return false
}

func (mv *messageValidator) validConsensusSigners(share *ssvtypes.SSVShare, m *specqbft.SignedMessage) error {
	switch {
	case len(m.Signers) == 0:
		return ErrNoSigners

	case len(m.Signers) == 1:
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

	case m.Message.MsgType != specqbft.CommitMsgType:
		e := ErrNonDecidedWithMultipleSigners
		e.got = len(m.Signers)
		return e

	case !share.HasQuorum(len(m.Signers)) || len(m.Signers) > len(share.Committee):
		e := ErrWrongSignersLength
		e.want = fmt.Sprintf("between %v and %v", share.Quorum, len(share.Committee))
		e.got = len(m.Signers)
		return e
	}

	if !slices.IsSorted(m.Signers) {
		return ErrSignersNotSorted
	}

	var prevSigner spectypes.OperatorID
	for _, signer := range m.Signers {
		if err := mv.commonSignerValidation(signer, share); err != nil {
			return err
		}
		if signer == prevSigner {
			return ErrDuplicatedSigner
		}
		prevSigner = signer
	}
	return nil
}
