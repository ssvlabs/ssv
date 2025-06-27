package validation

// partial_validation.go contains methods for validating partial signature messages

import (
	"fmt"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func (mv *messageValidator) validatePartialSignatureMessage(
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	receivedFrom peer.ID,
	receivedAt time.Time,
) (
	*spectypes.PartialSignatureMessages,
	error,
) {
	ssvMessage := signedSSVMessage.SSVMessage

	if len(ssvMessage.Data) > maxEncodedPartialSignatureSize {
		e := ErrSSVDataTooBig
		e.got = len(ssvMessage.Data)
		e.want = maxEncodedPartialSignatureSize
		return nil, e
	}

	partialSignatureMessages := &spectypes.PartialSignatureMessages{}
	if err := partialSignatureMessages.Decode(ssvMessage.Data); err != nil {
		e := ErrUndecodableMessageData
		e.innerErr = err
		return nil, e
	}

	if err := mv.validatePartialSignatureMessageSemantics(signedSSVMessage, partialSignatureMessages, committeeInfo.validatorIndices); err != nil {
		return nil, err
	}

	key := peerIDWithMessageID{
		peerID:    receivedFrom,
		messageID: ssvMessage.GetID(),
	}
	state := mv.validatorState(key, committeeInfo.committee)
	if err := mv.validatePartialSigMessagesByDutyLogic(signedSSVMessage, partialSignatureMessages, committeeInfo, receivedAt, state); err != nil {
		return nil, err
	}

	signature := signedSSVMessage.Signatures[0]
	signer := signedSSVMessage.OperatorIDs[0]
	if err := mv.signatureVerifier.VerifySignature(signer, ssvMessage, signature); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", signer, err)
		return partialSignatureMessages, e
	}

	if err := mv.updatePartialSignatureState(partialSignatureMessages, state, signer, committeeInfo); err != nil {
		return nil, err
	}

	return partialSignatureMessages, nil
}

func (mv *messageValidator) validatePartialSignatureMessageSemantics(
	signedSSVMessage *spectypes.SignedSSVMessage,
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	validatorIndices []phase0.ValidatorIndex,
) error {
	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()

	// Rule: Partial Signature message must have 1 signer
	signers := signedSSVMessage.OperatorIDs
	if len(signers) != 1 {
		return ErrPartialSigOneSigner
	}

	signer := signers[0]

	// Rule: Partial signature message must not have full data
	if len(signedSSVMessage.FullData) > 0 {
		return ErrFullDataNotInConsensusMessage
	}

	// Rule: Valid signature type
	if !mv.validPartialSigMsgType(partialSignatureMessages.Type) {
		e := ErrInvalidPartialSignatureType
		e.got = partialSignatureMessages.Type
		return e
	}

	// Rule: Partial signature type must match expected type:
	// - PostConsensusPartialSig, for Committee duty
	// - RandaoPartialSig or PostConsensusPartialSig for Proposer
	// - SelectionProofPartialSig or PostConsensusPartialSig for Aggregator
	// - SelectionProofPartialSig or PostConsensusPartialSig for Sync committee contribution
	// - ValidatorRegistrationPartialSig for Validator Registration
	// - VoluntaryExitPartialSig for Voluntary Exit
	if !mv.partialSignatureTypeMatchesRole(partialSignatureMessages.Type, role) {
		return ErrPartialSignatureTypeRoleMismatch
	}

	// Rule: Partial signature message must have at least one signature
	if len(partialSignatureMessages.Messages) == 0 {
		return ErrNoPartialSignatureMessages
	}

	for _, message := range partialSignatureMessages.Messages {
		// Rule: Partial signature must have expected length. Already enforced by ssz.

		// Rule: Partial signature signer must be consistent
		if message.Signer != signer {
			err := ErrInconsistentSigners
			err.got = signer
			err.want = message.Signer
			return err
		}

		// Rule: (only for Validator duties) Validator index must match with validatorPK
		// For Committee duties, we don't assume that operators are synced on the validators set
		// So, we can't make this assertion
		if !mv.committeeRole(signedSSVMessage.SSVMessage.GetID().GetRoleType()) {
			if !slices.Contains(validatorIndices, message.ValidatorIndex) {
				e := ErrValidatorIndexMismatch
				e.got = message.ValidatorIndex
				e.want = validatorIndices
				return e
			}
		}
	}

	return nil
}

func (mv *messageValidator) validatePartialSigMessagesByDutyLogic(
	signedSSVMessage *spectypes.SignedSSVMessage,
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	committeeInfo CommitteeInfo,
	receivedAt time.Time,
	state *ValidatorState,
) error {
	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()
	messageSlot := partialSignatureMessages.Slot
	signer := signedSSVMessage.OperatorIDs[0]
	signerStateBySlot := state.Signer(committeeInfo.signerIndex(signer))

	// Rule: Height must not be "old". I.e., signer must not have already advanced to a later slot.
	if role != spectypes.RoleCommittee { // Rule only for validator runners
		maxSlot := signerStateBySlot.MaxSlot()
		if maxSlot != 0 && maxSlot > partialSignatureMessages.Slot {
			e := ErrSlotAlreadyAdvanced
			e.got = partialSignatureMessages.Slot
			e.want = maxSlot
			return e
		}
	}

	randaoMsg := partialSignatureMessages.Type == spectypes.RandaoPartialSig
	if err := mv.validateBeaconDuty(signedSSVMessage.SSVMessage.GetID().GetRoleType(), messageSlot, committeeInfo.validatorIndices, randaoMsg); err != nil {
		return err
	}

	if signerState := signerStateBySlot.GetSignerState(messageSlot); signerState != nil {
		// Rule: peer must send only:
		// - 1 PostConsensusPartialSig, for Committee duty
		// - 1 RandaoPartialSig and 1 PostConsensusPartialSig for Proposer
		// - 1 SelectionProofPartialSig and 1 PostConsensusPartialSig for Aggregator
		// - 1 SelectionProofPartialSig and 1 PostConsensusPartialSig for Sync committee contribution
		// - 1 ValidatorRegistrationPartialSig for Validator Registration
		// - 1 VoluntaryExitPartialSig for Voluntary Exit
		if err := signerState.SeenMsgTypes.ValidatePartialSignatureMessage(partialSignatureMessages); err != nil {
			return err
		}
	}

	// Rule: current slot must be between duty's starting slot and:
	// - duty's starting slot + 34 (committee and aggregation)
	// - duty's starting slot + 3 (other duties)
	if err := mv.validateSlotTime(messageSlot, role, receivedAt); err != nil {
		return err
	}

	// Rule: valid number of duties per epoch:
	// - 2 for aggregation, voluntary exit and validator registration
	// - 2*V for Committee duty (where V is the number of validators in the cluster) (if no validator is doing sync committee in this epoch)
	// - else, accept
	if err := mv.validateDutyCount(signedSSVMessage.SSVMessage.GetID(), messageSlot, committeeInfo.validatorIndices, signerStateBySlot); err != nil {
		return err
	}

	clusterValidatorCount := len(committeeInfo.validatorIndices)
	partialSignatureMessageCount := len(partialSignatureMessages.Messages)

	if signedSSVMessage.SSVMessage.MsgID.GetRoleType() == spectypes.RoleCommittee {
		// Rule: The number of signatures must be <= min(2*V, V + SYNC_COMMITTEE_SIZE) where V is the number of validators assigned to the cluster
		// #nosec G115
		if partialSignatureMessageCount > min(2*clusterValidatorCount, clusterValidatorCount+int(mv.netCfg.GetSyncCommitteeSize())) {
			return ErrTooManyPartialSignatureMessages
		}

		// Rule: a ValidatorIndex can't appear more than 2 times in the []*PartialSignatureMessage list
		validatorIndexCount := make(map[phase0.ValidatorIndex]int)
		for _, message := range partialSignatureMessages.Messages {
			validatorIndexCount[message.ValidatorIndex]++
			if validatorIndexCount[message.ValidatorIndex] > 2 {
				return ErrTripleValidatorIndexInPartialSignatures
			}
		}
	} else if signedSSVMessage.SSVMessage.MsgID.GetRoleType() == spectypes.RoleSyncCommitteeContribution {
		// Rule: The number of signatures must be <= MaxSignaturesInSyncCommitteeContribution for the sync committee contribution duty
		if partialSignatureMessageCount > maxOperators {
			e := ErrTooManyPartialSignatureMessages
			e.got = partialSignatureMessageCount
			e.want = maxOperators
			return e
		}
	} else if partialSignatureMessageCount > 1 {
		// Rule: The number of signatures must be 1 for the other types of duties
		e := ErrTooManyPartialSignatureMessages
		e.got = partialSignatureMessageCount
		e.want = 1
		return e
	}

	return nil
}

func (mv *messageValidator) updatePartialSignatureState(
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	state *ValidatorState,
	signer spectypes.OperatorID,
	committeeInfo CommitteeInfo,
) error {
	stateBySlot := state.Signer(committeeInfo.signerIndex(signer))
	messageSlot := partialSignatureMessages.Slot
	messageEpoch := mv.netCfg.EstimatedEpochAtSlot(messageSlot)

	signerState := stateBySlot.GetSignerState(messageSlot)
	if signerState == nil {
		signerState = newSignerState(messageSlot, specqbft.FirstRound)
		stateBySlot.SetSignerState(messageSlot, messageEpoch, signerState)
	}

	return signerState.SeenMsgTypes.RecordPartialSignatureMessage(partialSignatureMessages)
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
