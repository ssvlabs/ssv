package msgvalidation

// partial_validation.go contains methods for validating partial signature messages

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/alan/qbft"
	"github.com/bloxapp/ssv-spec/alan/types"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	"golang.org/x/exp/slices"
)

func (mv *messageValidator) validatePartialSignatureMessage(
	signedSSVMessage *spectypes.SignedSSVMessage,
	committee []spectypes.OperatorID,
	validatorIndices []phase0.ValidatorIndex,
	receivedAt time.Time,
) (
	*spectypes.PartialSignatureMessages,
	error,
) {
	ssvMessage := signedSSVMessage.GetSSVMessage()

	if len(ssvMessage.Data) > maxPartialSignatureMsgSize {
		e := ErrSSVDataTooBig
		e.got = len(ssvMessage.Data)
		e.want = maxPartialSignatureMsgSize
		return nil, e
	}

	partialSignatureMessages := &spectypes.PartialSignatureMessages{}
	if err := partialSignatureMessages.Decode(ssvMessage.Data); err != nil {
		e := ErrUndecodableMessageData
		e.innerErr = err
		return nil, e
	}

	mv.reportPartialSignatureMessageMetrics(committee)

	if err := mv.validatePartialSignatureMessageSemantics(signedSSVMessage, partialSignatureMessages, committee); err != nil {
		return nil, err
	}

	msgID := ssvMessage.GetID()
	state := mv.consensusState(msgID)
	if err := mv.validatePartialSigMessagesByDutyLogic(signedSSVMessage, partialSignatureMessages, committee, validatorIndices, state); err != nil {
		return nil, err
	}

	msgSlot := partialSignatureMessages.Slot

	if !mv.validPartialSigMsgType(partialSignatureMessages.Type) {
		e := ErrUnknownPartialMessageType
		e.got = partialSignatureMessages.Type
		return partialSignatureMessages, e
	}

	role := msgID.GetRoleType()

	if !mv.partialSignatureTypeMatchesRole(partialSignatureMessages.Type, role) {
		return partialSignatureMessages, ErrPartialSignatureTypeRoleMismatch
	}

	if err := mv.validateSlotTime(msgSlot, role, receivedAt); err != nil {
		return partialSignatureMessages, err
	}

	signers := signedSSVMessage.GetOperatorIDs()
	if len(signers) > 1 {
		return partialSignatureMessages, ErrPartialSigOneSigner
	}

	signer := signers[0]
	signature := signedSSVMessage.GetSignature()[0]

	if err := mv.validatePartialMessages(partialSignatureMessages, signer, validatorIndices); err != nil {
		return partialSignatureMessages, err
	}

	signerState := state.GetSignerState(signer)
	if signerState != nil {
		if err := mv.validateSignerBehaviorPartial(state, signer, committee, msgID, partialSignatureMessages); err != nil {
			return partialSignatureMessages, err
		}
	}

	if err := mv.verifySignature(ssvMessage, signer, signature); err != nil {
		return partialSignatureMessages, err
	}

	mv.updatePartialSignatureState(partialSignatureMessages, signerState)

	return partialSignatureMessages, nil
}

func (mv *messageValidator) reportPartialSignatureMessageMetrics(committee []spectypes.OperatorID) {
	if mv.operatorDataStore != nil && mv.operatorDataStore.OperatorIDReady() {
		if mv.ownCommittee(committee) {
			mv.metrics.CommitteeMessage(spectypes.SSVPartialSignatureMsgType, false)
		} else {
			mv.metrics.NonCommitteeMessage(spectypes.SSVPartialSignatureMsgType, false)
		}
	}
}

func (mv *messageValidator) validatePartialSignatureMessageSemantics(
	signedSSVMessage *spectypes.SignedSSVMessage,
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	committee []spectypes.OperatorID,
) error {

	return nil
}

func (mv *messageValidator) validatePartialSigMessagesByDutyLogic(
	signedSSVMessage *spectypes.SignedSSVMessage,
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	committee []spectypes.OperatorID,
	validatorIndices []phase0.ValidatorIndex,
	state *consensusState,
) error {
	messageSlot := partialSignatureMessages.Slot

	if err := mv.validateBeaconDuty(signedSSVMessage.GetSSVMessage().GetID().GetRoleType(), messageSlot, validatorIndices); err != nil {
		return err
	}

	signerState := state.GetSignerState(signedSSVMessage.GetOperatorIDs()[0])

	messageEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(messageSlot)
	stateEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot)

	if messageSlot <= signerState.Slot {
		limits := maxMessageCounts(len(committee))
		if err := signerState.MessageCounts.ValidatePartialSignatureMessage(partialSignatureMessages, limits); err != nil {
			return err
		}
	}

	if messageSlot < signerState.Slot {
		// Signers aren't allowed to decrease their slot.
		// If they've sent a future message due to clock error,
		// this should be caught by the earlyMessage check.
		err := ErrSlotAlreadyAdvanced
		err.want = signerState.Slot
		err.got = messageSlot
		return err
	}

	newDutyInSameEpoch := false
	if messageSlot > signerState.Slot && messageEpoch == stateEpoch {
		newDutyInSameEpoch = true
	}

	if err := mv.validateDutyCount(signerState, signedSSVMessage.GetSSVMessage().GetID(), newDutyInSameEpoch); err != nil {
		return err
	}

	if signedSSVMessage.SSVMessage.MsgID.GetRoleType() == spectypes.RoleCommittee {

		// Rule: The number of signatures must be <= min(2*V, V + SYNC_COMMITTEE_SIZE) where V is the number of validators assigned to the cluster
		if !mv.ValidNumberOfSignaturesForCommitteeDuty(signedSSVMessage.SSVMessage.MsgID.GetSenderID(), &partialSignatureMessages) {
			return ErrTooManySignatures
		}

		// Rule: a ValidatorIndex can't appear more than 2 times in the []*PartialSignatureMessage list
		if !mv.NoTripleValidatorOccurrence(&partialSignatureMessages) {
			return ErrTripleValidatorIndexInSignatures
		}
	} else if signedSSVMessage.SSVMessage.MsgID.GetRoleType() == types.RoleSyncCommitteeContribution {
		// Rule: The number of signatures must be <= MaxSignaturesInSyncCommitteeContribution for the sync comittee contribution duty
		if len(partialSignatureMessages.Messages) > MaxSignaturesInSyncCommitteeContribution {
			return ErrTooManySignatures
		}
	} else {
		// Rule: The number of signatures must be 1 for the other types of duties
		if len(partialSignatureMessages.Messages) > 1 {
			return ErrTooManySignatures
		}
	}

	return nil
}

func (mv *messageValidator) updatePartialSignatureState(partialSignatureMessages *spectypes.PartialSignatureMessages, signerState *SignerState) {
	if partialSignatureMessages.Slot > signerState.Slot {
		newEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(partialSignatureMessages.Slot) > mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot)
		signerState.ResetSlot(partialSignatureMessages.Slot, specqbft.FirstRound, newEpoch)
	}

	signerState.MessageCounts.RecordPartialSignatureMessage(partialSignatureMessages)
}

func (mv *messageValidator) validPartialSigMsgType(msgType spectypes.PartialSigMsgType) bool {
	switch msgType {
	case spectypes.PostConsensusPartialSig,
		spectypes.RandaoPartialSig,
		spectypes.SelectionProofPartialSig,
		spectypes.ContributionProofs,
		spectypes.ValidatorRegistrationPartialSig,
		spectypes.VoluntaryExitPartialSig:
		return true
	default:
		return false
	}
}

func (mv *messageValidator) partialSignatureTypeMatchesRole(msgType spectypes.PartialSigMsgType, role spectypes.RunnerRole) bool {
	switch role {
	case spectypes.RoleCommittee:
		return msgType == spectypes.PostConsensusPartialSig
	case spectypes.RoleAggregator:
		return msgType == spectypes.PostConsensusPartialSig || msgType == spectypes.SelectionProofPartialSig
	case spectypes.RoleProposer:
		return msgType == spectypes.PostConsensusPartialSig || msgType == spectypes.RandaoPartialSig
	case spectypes.RoleSyncCommitteeContribution:
		return msgType == spectypes.PostConsensusPartialSig || msgType == spectypes.ContributionProofs
	case spectypes.RoleValidatorRegistration:
		return msgType == spectypes.ValidatorRegistrationPartialSig
	case spectypes.RoleVoluntaryExit:
		return msgType == spectypes.VoluntaryExitPartialSig
	default:
		return false
	}
}

func (mv *messageValidator) validatePartialMessages(
	messages *spectypes.PartialSignatureMessages,
	signer spectypes.OperatorID,
	validatorIndices []phase0.ValidatorIndex,
) error {
	if len(messages.Messages) == 0 {
		return ErrNoPartialMessages
	}

	seen := map[[32]byte]struct{}{}
	for _, message := range messages.Messages {
		if _, ok := seen[message.SigningRoot]; ok {
			return ErrDuplicatedPartialSignatureMessage
		}
		seen[message.SigningRoot] = struct{}{}

		if message.Signer != signer {
			err := ErrUnexpectedSigner
			err.want = signer
			err.got = message.Signer
			return err
		}

		if err := mv.validateBLSSignatureFormat(message.PartialSignature); err != nil {
			return err
		}

		if !slices.Contains(validatorIndices, message.ValidatorIndex) {
			e := ErrPartialSignatureValidatorIndexNotFound
			e.got = message.ValidatorIndex
			e.want = validatorIndices
			return e
		}
	}

	return nil
}

func (mv *messageValidator) validateBLSSignatureFormat(signature []byte) error {
	if len(signature) != blsSignatureSize {
		e := ErrWrongBLSSignatureSize
		e.got = len(signature)
		return e
	}

	if [blsSignatureSize]byte(signature) == [blsSignatureSize]byte{} {
		return ErrEmptySignature
	}
	return nil
}

func (mv *messageValidator) validateSignerBehaviorPartial(
	state *consensusState,
	signer spectypes.OperatorID,
	committee []spectypes.OperatorID,
	msgID spectypes.MessageID,
	messages *spectypes.PartialSignatureMessages,
) error {
	signerState := state.GetSignerState(signer)

	if signerState == nil {
		return nil
	}

	msgSlot := messages.Slot

	//if msgSlot < signerState.Slot {
	//	// Signers aren't allowed to decrease their slot.
	//	// If they've sent a future message due to clock error,
	//	// this should be caught by the earlyMessage check.
	//	err := ErrSlotAlreadyAdvanced
	//	err.want = signerState.Slot
	//	err.got = msgSlot
	//	return err
	//}

	//newDutyInSameEpoch := false
	//if msgSlot > signerState.Slot && mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) == mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot) {
	//	newDutyInSameEpoch = true
	//}
	//
	//if err := mv.validateDutyCount(signerState, msgID, newDutyInSameEpoch); err != nil {
	//	return err
	//}

	//if msgSlot <= signerState.Slot {
	//	limits := maxMessageCounts(len(committee))
	//	if err := signerState.MessageCounts.ValidatePartialSignatureMessage(messages, limits); err != nil {
	//		return err
	//	}
	//}

	return nil
}
