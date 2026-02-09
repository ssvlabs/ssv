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
		return partialSignatureMessages, err
	}

	state := mv.validatorState(ssvMessage.GetID(), committeeInfo)
	if err := mv.validatePartialSigMessagesByDutyLogic(signedSSVMessage, partialSignatureMessages, committeeInfo, receivedFrom, receivedAt, state); err != nil {
		return partialSignatureMessages, err
	}

	signature := signedSSVMessage.Signatures[0]
	signer := signedSSVMessage.OperatorIDs[0]
	if err := mv.signatureVerifier.VerifySignature(signer, ssvMessage, signature); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", signer, err)
		return partialSignatureMessages, e
	}

	if err := mv.updatePartialSignatureState(partialSignatureMessages, receivedFrom, state, signer, committeeInfo); err != nil {
		return partialSignatureMessages, err
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
		return ErrPartialSigMessageMustHaveOneSigner
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
		return ErrNoMessagesInPartialSigMessage
	}

	for _, message := range partialSignatureMessages.Messages {
		// Rule: Partial signature must have expected length. Already enforced by ssz.

		// Rule: Partial signature signer must be consistent
		if message.Signer != signer {
			e := ErrInconsistentSigners
			e.got = signer
			e.want = message.Signer
			return e
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
	receivedFrom peer.ID,
	receivedAt time.Time,
	state *ValidatorState,
) error {
	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()
	messageSlot := partialSignatureMessages.Slot
	signer := signedSSVMessage.OperatorIDs[0]
	operatorState := state.OperatorState(committeeInfo.signerIndex(signer))

	// Rule: Height must not be "old". I.e., signer must not have already advanced to a later slot.
	if role != spectypes.RoleCommittee { // Rule only for validator runners
		maxSlot := operatorState.MaxSlot()
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

	if signerState := operatorState.GetSignerStateForSlot(messageSlot); signerState != nil {
		// Rule: Expect to receive at most:
		// - 1 PostConsensusPartialSig, for Committee duty
		// - 1 RandaoPartialSig and 1 PostConsensusPartialSig for Proposer
		// - 1 SelectionProofPartialSig and 1 PostConsensusPartialSig for Aggregator
		// - 1 SelectionProofPartialSig and 1 PostConsensusPartialSig for Sync committee contribution
		// - 1 ValidatorRegistrationPartialSig for Validator Registration
		// - 1 VoluntaryExitPartialSig for Voluntary Exit
		if err := validatePartialSignatureMessageLimit(partialSignatureMessages, receivedFrom, signerState); err != nil {
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
	if err := mv.validateDutyCount(signedSSVMessage.SSVMessage.GetID(), messageSlot, committeeInfo.validatorIndices, operatorState); err != nil {
		return err
	}

	clusterValidatorCount := len(committeeInfo.validatorIndices)
	partialSignatureMessageCount := len(partialSignatureMessages.Messages)

	if signedSSVMessage.SSVMessage.MsgID.GetRoleType() == spectypes.RoleCommittee {
		// Rule: The number of signatures must be <= min(2*V, V + SYNC_COMMITTEE_SIZE) where V is the number of validators assigned to the cluster
		// #nosec G115
		if partialSignatureMessageCount > min(2*clusterValidatorCount, clusterValidatorCount+int(mv.netCfg.SyncCommitteeSize)) {
			return ErrTooManySignaturesInPartialSigMessage
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
		if partialSignatureMessageCount > maxSignatures {
			e := ErrTooManySignaturesInPartialSigMessage
			e.got = partialSignatureMessageCount
			e.want = maxSignatures
			return e
		}
	} else if partialSignatureMessageCount > 1 {
		// Rule: The number of signatures must be 1 for the other types of duties
		e := ErrTooManySignaturesInPartialSigMessage
		e.got = partialSignatureMessageCount
		e.want = 1
		return e
	}

	return nil
}

// validatePartialSignatureMessageLimit checks if the provided partial signature message exceeds the set limits.
// Returns an error if the message type exceeds its respective count limit.
func validatePartialSignatureMessageLimit(
	m *spectypes.PartialSignatureMessages,
	receivedFrom peer.ID,
	signerState *SignerStateForSlotRound,
) error {
	switch m.Type {
	case spectypes.RandaoPartialSig, spectypes.SelectionProofPartialSig, spectypes.ContributionProofs,
		spectypes.ValidatorRegistrationPartialSig, spectypes.VoluntaryExitPartialSig:
		if signerState.Peer(receivedFrom).SeenMsgTypes.reachedPreConsensusLimit() {
			// Check if the same peer is sending us a "logical duplicate" message, reject message to punish.
			e := ErrTooManyPartialSigMessage
			e.reject = true
			e.got = fmt.Sprintf("pre-consensus, having %v", signerState.Peer(receivedFrom).SeenMsgTypes.String())
			return e
		}
		if signerState.World.SeenMsgTypes.reachedPreConsensusLimit() {
			// Check if a different peer is sending us a "logical duplicate" message, ignore message since this
			// is expected occasionally.
			e := ErrTooManyPartialSigMessage
			e.got = fmt.Sprintf("pre-consensus, having %v", signerState.World.SeenMsgTypes.String())
			return e
		}
	case spectypes.PostConsensusPartialSig:
		if signerState.Peer(receivedFrom).SeenMsgTypes.reachedPostConsensusLimit() {
			// Check if the same peer is sending us a "logical duplicate" message, reject message to punish.
			e := ErrTooManyPartialSigMessage
			e.reject = true
			e.got = fmt.Sprintf("post-consensus, having %v", signerState.Peer(receivedFrom).SeenMsgTypes.String())
			return e
		}
		if signerState.World.SeenMsgTypes.reachedPostConsensusLimit() {
			// Check if a different peer is sending us a "logical duplicate" message, ignore message since this
			// is expected occasionally.
			e := ErrTooManyPartialSigMessage
			e.got = fmt.Sprintf("post-consensus, having %v", signerState.World.SeenMsgTypes.String())
			return e
		}
	default:
		return fmt.Errorf("unexpected partial signature message type: %d", m.Type)
	}

	return nil
}

func (mv *messageValidator) updatePartialSignatureState(
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	receivedFrom peer.ID,
	state *ValidatorState,
	signer spectypes.OperatorID,
	committeeInfo CommitteeInfo,
) error {
	messageSlot := partialSignatureMessages.Slot
	messageEpoch := mv.netCfg.EstimatedEpochAtSlot(messageSlot)

	operatorState := state.OperatorState(committeeInfo.signerIndex(signer))

	signerState := operatorState.GetSignerStateForSlot(messageSlot)
	if signerState == nil {
		signerState = newSignerState(messageSlot, specqbft.FirstRound)
		operatorState.SetSignerStateForSlot(messageSlot, messageEpoch, signerState)
	}

	err := signerState.Peer(receivedFrom).SeenMsgTypes.RecordPartialSignatureMessage(partialSignatureMessages)
	if err != nil {
		return err
	}
	err = signerState.World.SeenMsgTypes.RecordPartialSignatureMessage(partialSignatureMessages)
	if err != nil {
		return err
	}

	return nil
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
