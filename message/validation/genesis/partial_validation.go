package validation

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// partial_validation.go contains methods for validating partial signature messages

func (mv *messageValidator) validatePartialSignatureMessage(
	share *ssvtypes.SSVShare,
	signedMsg *genesisspectypes.SignedPartialSignatureMessage,
	msgID genesisspectypes.MessageID,
	signatureVerifier func() error,
	receivedAt time.Time,
) (phase0.Slot, error) {
	msgSlot := signedMsg.Message.Slot

	if !mv.validPartialSigMsgType(signedMsg.Message.Type) {
		e := ErrUnknownPartialMessageType
		e.got = signedMsg.Message.Type
		return msgSlot, e
	}

	role := msgID.GetRoleType()
	if !mv.partialSignatureTypeMatchesRole(signedMsg.Message.Type, role) {
		return msgSlot, ErrPartialSignatureTypeRoleMismatch
	}

	if err := mv.validatePartialMessages(share, signedMsg); err != nil {
		return msgSlot, err
	}

	if err := mv.validateSlotTime(msgSlot, role, receivedAt); err != nil {
		return msgSlot, err
	}

	state := mv.consensusState(msgID)
	signerState := state.GetSignerState(signedMsg.Signer)
	if signerState != nil {
		if err := mv.validateSignerBehaviorPartial(state, signedMsg.Signer, share, msgID, signedMsg); err != nil {
			return msgSlot, err
		}
	}

	if err := mv.validateSignatureFormat(signedMsg.Signature); err != nil {
		return msgSlot, err
	}

	if signatureVerifier != nil {
		if err := signatureVerifier(); err != nil {
			return msgSlot, err
		}
	}

	if signerState == nil {
		signerState = state.CreateSignerState(signedMsg.Signer)
	}

	if msgSlot > signerState.Slot {
		newEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot) > mv.netCfg.Beacon.EstimatedEpochAtSlot(signerState.Slot)
		signerState.ResetSlot(msgSlot, genesisspecqbft.FirstRound, newEpoch)
	}

	signerState.MessageCounts.RecordPartialSignatureMessage(signedMsg)

	return msgSlot, nil
}

func (mv *messageValidator) validPartialSigMsgType(msgType genesisspectypes.PartialSigMsgType) bool {
	switch msgType {
	case genesisspectypes.PostConsensusPartialSig,
		genesisspectypes.RandaoPartialSig,
		genesisspectypes.SelectionProofPartialSig,
		genesisspectypes.ContributionProofs,
		genesisspectypes.ValidatorRegistrationPartialSig,
		genesisspectypes.VoluntaryExitPartialSig:
		return true
	default:
		return false
	}
}

func (mv *messageValidator) partialSignatureTypeMatchesRole(msgType genesisspectypes.PartialSigMsgType, role genesisspectypes.BeaconRole) bool {
	switch role {
	case genesisspectypes.BNRoleAttester:
		return msgType == genesisspectypes.PostConsensusPartialSig
	case genesisspectypes.BNRoleAggregator:
		return msgType == genesisspectypes.PostConsensusPartialSig || msgType == genesisspectypes.SelectionProofPartialSig
	case genesisspectypes.BNRoleProposer:
		return msgType == genesisspectypes.PostConsensusPartialSig || msgType == genesisspectypes.RandaoPartialSig
	case genesisspectypes.BNRoleSyncCommittee:
		return msgType == genesisspectypes.PostConsensusPartialSig
	case genesisspectypes.BNRoleSyncCommitteeContribution:
		return msgType == genesisspectypes.PostConsensusPartialSig || msgType == genesisspectypes.ContributionProofs
	case genesisspectypes.BNRoleValidatorRegistration:
		return msgType == genesisspectypes.ValidatorRegistrationPartialSig
	case genesisspectypes.BNRoleVoluntaryExit:
		return msgType == genesisspectypes.VoluntaryExitPartialSig
	default:
		panic("invalid role") // role validity should be checked before
	}
}

func (mv *messageValidator) validatePartialMessages(share *ssvtypes.SSVShare, m *genesisspectypes.SignedPartialSignatureMessage) error {
	if err := mv.commonSignerValidation(m.Signer, share); err != nil {
		return err
	}

	if len(m.Message.Messages) == 0 {
		return ErrNoPartialMessages
	}

	seen := map[[32]byte]struct{}{}
	for _, message := range m.Message.Messages {
		if _, ok := seen[message.SigningRoot]; ok {
			return ErrDuplicatedPartialSignatureMessage
		}
		seen[message.SigningRoot] = struct{}{}

		if message.Signer != m.Signer {
			err := ErrUnexpectedSigner
			err.want = m.Signer
			err.got = message.Signer
			return err
		}

		if err := mv.commonSignerValidation(message.Signer, share); err != nil {
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
	signer genesisspectypes.OperatorID,
	share *ssvtypes.SSVShare,
	msgID genesisspectypes.MessageID,
	signedMsg *genesisspectypes.SignedPartialSignatureMessage,
) error {
	signerState := state.GetSignerState(signer)

	if signerState == nil {
		return nil
	}

	msgSlot := signedMsg.Message.Slot

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

	if err := mv.validateBeaconDuty(msgID.GetRoleType(), msgSlot, share); err != nil {
		return err
	}

	if err := mv.validateDutyCount(signerState, msgID, newDutyInSameEpoch); err != nil {
		return err
	}

	if msgSlot <= signerState.Slot {
		limits := maxMessageCounts(len(share.Committee))
		if err := signerState.MessageCounts.ValidatePartialSignatureMessage(signedMsg, limits); err != nil {
			return err
		}
	}

	return nil
}
