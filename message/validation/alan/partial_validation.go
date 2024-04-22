package validation

// partial_validation.go contains methods for validating partial signature messages

import (
	specqbft "github.com/bloxapp/ssv-spec/alan/qbft"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
)

func (mv *messageValidator) validatePartialSignatureMessage(
	signedSSVMessage *spectypes.SignedSSVMessage,
	committee []spectypes.OperatorID,
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
		e := ErrMalformedMessage
		e.innerErr = err
		return nil, e
	}

	//if mv.operatorDataStore != nil && mv.operatorDataStore.OperatorIDReady() {
	//	if mv.inCommittee(share) {
	//		mv.metrics.InCommitteeMessage(spectypes.SSVPartialSignatureMsgType, false)
	//	} else {
	//		mv.metrics.NonCommitteeMessage(spectypes.SSVPartialSignatureMsgType, false)
	//	}
	//}

	msgSlot := partialSignatureMessages.Slot

	if !mv.validPartialSigMsgType(partialSignatureMessages.Type) {
		e := ErrUnknownPartialMessageType
		e.got = partialSignatureMessages.Type
		return partialSignatureMessages, e
	}

	msgID := ssvMessage.GetID()
	role := msgID.GetRoleType()

	if !mv.partialSignatureTypeMatchesRole(partialSignatureMessages.Type, role) {
		return partialSignatureMessages, ErrPartialSignatureTypeRoleMismatch
	}

	// TODO: make sure there's only one signer and only one signature

	signer := signedSSVMessage.GetOperatorIDs()[0]
	signature := signedSSVMessage.GetSignature()[0]

	state := mv.consensusState(msgID)
	if err := mv.validatePartialMessages(committee, partialSignatureMessages, signer); err != nil {
		return partialSignatureMessages, err
	}

	signerState := state.GetSignerState(signer)
	if signerState != nil {
		if err := mv.validateSignerBehaviorPartial(state, signer, committee, msgID, partialSignatureMessages); err != nil {
			return partialSignatureMessages, err
		}
	}

	if err := mv.validateSignatureFormat(signature); err != nil {
		return partialSignatureMessages, err
	}

	if err := mv.verifySignature(ssvMessage, signer, signature); err != nil {
		return partialSignatureMessages, err
	}

	if signerState == nil {
		signerState = state.CreateSignerState(signer)
	}

	if msgSlot > signerState.Slot {
		newEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) > mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot)
		signerState.ResetSlot(msgSlot, specqbft.FirstRound, newEpoch)
	}

	signerState.MessageCounts.RecordPartialSignatureMessage(partialSignatureMessages)

	return partialSignatureMessages, nil
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

func (mv *messageValidator) validatePartialMessages(committee []spectypes.OperatorID, messages *spectypes.PartialSignatureMessages, signer spectypes.OperatorID) error {
	if err := mv.commonSignerValidation(signer, committee); err != nil {
		return err
	}

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

		if err := mv.commonSignerValidation(message.Signer, committee); err != nil {
			return err
		}

		if err := mv.validateSignatureFormat(message.PartialSignature); err != nil {
			return err
		}
	}

	return nil
}

func (mv *messageValidator) validateSignerBehaviorPartial(
	state *ConsensusState,
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

	if msgSlot < signerState.Slot {
		// Signers aren't allowed to decrease their slot.
		// If they've sent a future message due to clock error,
		// this should be caught by the earlyMessage check.
		err := ErrSlotAlreadyAdvanced
		err.want = signerState.Slot
		err.got = msgSlot
		return err
	}

	newDutyInSameEpoch := false
	if msgSlot > signerState.Slot && mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) == mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot) {
		newDutyInSameEpoch = true
	}

	if err := mv.validateDutyCount(signerState, msgID, newDutyInSameEpoch); err != nil {
		return err
	}

	if msgSlot <= signerState.Slot {
		limits := maxMessageCounts(len(committee))
		if err := signerState.MessageCounts.ValidatePartialSignatureMessage(messages, limits); err != nil {
			return err
		}
	}

	return nil
}
