package validation

// partial_validation.go contains methods for validating partial signature messages

import (
	"fmt"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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

	if err := mv.validatePartialSignatureMessageSemantics(signedSSVMessage, partialSignatureMessages, validatorIndices); err != nil {
		return nil, err
	}

	msgID := ssvMessage.GetID()
	state := mv.consensusState(msgID)
	if err := mv.validatePartialSigMessagesByDutyLogic(signedSSVMessage, partialSignatureMessages, committee, validatorIndices, receivedAt, state); err != nil {
		return nil, err
	}

	signature := signedSSVMessage.GetSignature()[0]
	signer := signedSSVMessage.GetOperatorIDs()[0]
	if err := mv.signatureVerifier.VerifySignature(signer, ssvMessage, signature); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", signer, err)
		return partialSignatureMessages, e
	}

	mv.updatePartialSignatureState(partialSignatureMessages, state.GetSignerState(signer))

	return partialSignatureMessages, nil
}

func (mv *messageValidator) validatePartialSignatureMessageSemantics(
	signedSSVMessage *spectypes.SignedSSVMessage,
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	validatorIndices []phase0.ValidatorIndex,
) error {
	role := signedSSVMessage.GetSSVMessage().GetID().GetRoleType()

	signers := signedSSVMessage.GetOperatorIDs()
	if len(signers) != 1 {
		return ErrPartialSigOneSigner
	}

	if len(signedSSVMessage.FullData) > 0 {
		return ErrFullDataNotInConsensusMessage
	}

	if !mv.validPartialSigMsgType(partialSignatureMessages.Type) {
		e := ErrInvalidPartialSignatureType
		e.got = partialSignatureMessages.Type
		return e
	}

	if !mv.partialSignatureTypeMatchesRole(partialSignatureMessages.Type, role) {
		return ErrPartialSignatureTypeRoleMismatch
	}

	if len(partialSignatureMessages.Messages) == 0 {
		return ErrNoPartialSignatureMessages
	}

	for _, message := range partialSignatureMessages.Messages {
		if len(message.PartialSignature) == 0 {
			return ErrEmptySignature
		}

		if message.Signer != signers[0] {
			err := ErrInconsistentSigners
			err.got = signers[0]
			err.want = message.Signer
			return err
		}

		if !slices.Contains(validatorIndices, message.ValidatorIndex) {
			e := ErrValidatorIndexMismatch
			e.got = message.ValidatorIndex
			e.want = validatorIndices
			return e
		}
	}

	return nil
}

func (mv *messageValidator) validatePartialSigMessagesByDutyLogic(
	signedSSVMessage *spectypes.SignedSSVMessage,
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	committee []spectypes.OperatorID,
	validatorIndices []phase0.ValidatorIndex,
	receivedAt time.Time,
	state *consensusState,
) error {
	role := signedSSVMessage.GetSSVMessage().GetID().GetRoleType()
	messageSlot := partialSignatureMessages.Slot

	if err := mv.validateBeaconDuty(signedSSVMessage.GetSSVMessage().GetID().GetRoleType(), messageSlot, validatorIndices); err != nil {
		return err
	}

	signer := signedSSVMessage.GetOperatorIDs()[0]
	signerState := state.GetSignerState(signer)
	if signerState == nil {
		signerState = state.CreateSignerState(signer)
	}

	if signerState != nil && messageSlot == signerState.Slot {
		limits := maxMessageCounts(len(committee))
		if err := signerState.MessageCounts.ValidatePartialSignatureMessage(partialSignatureMessages, limits); err != nil {
			return err
		}
	}

	if err := mv.validateSlotTime(messageSlot, role, receivedAt); err != nil {
		return err
	}

	messageEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(messageSlot)
	stateEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot)
	newDutyInSameEpoch := false
	if messageSlot > signerState.Slot && messageEpoch == stateEpoch {
		newDutyInSameEpoch = true
	}

	if err := mv.validateDutyCount(validatorIndices, signerState, signedSSVMessage.GetSSVMessage().GetID(), newDutyInSameEpoch); err != nil {
		return err
	}

	partialSignatureMessageCount := len(partialSignatureMessages.Messages)
	clusterValidatorCount := len(validatorIndices)

	if signedSSVMessage.SSVMessage.MsgID.GetRoleType() == spectypes.RoleCommittee {
		if partialSignatureMessageCount > min(2*clusterValidatorCount, clusterValidatorCount+syncCommitteeSize) {
			return ErrTooManyPartialSignatureMessages
		}

		validatorIndexCount := make(map[phase0.ValidatorIndex]int)
		for _, message := range partialSignatureMessages.Messages {
			validatorIndexCount[message.ValidatorIndex]++
			if validatorIndexCount[message.ValidatorIndex] > 2 {
				return ErrTripleValidatorIndexInPartialSignatures
			}
		}
	} else if signedSSVMessage.SSVMessage.MsgID.GetRoleType() == types.RoleSyncCommitteeContribution {
		if partialSignatureMessageCount > maxSignaturesInSyncCommitteeContribution {
			e := ErrTooManyPartialSignatureMessages
			e.got = partialSignatureMessageCount
			e.want = maxConsensusMsgSize
			return e
		}
	} else if partialSignatureMessageCount > 1 {
		e := ErrTooManyPartialSignatureMessages
		e.got = partialSignatureMessageCount
		e.want = 1
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

// TODO: delete after updating to Go 1.21
func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
