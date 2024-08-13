package validation

// consensus_validation.go contains methods for validating consensus messages

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func (mv *messageValidator) validateConsensusMessage(
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	receivedAt time.Time,
) (*specqbft.Message, error) {
	ssvMessage := signedSSVMessage.SSVMessage

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

	mv.metrics.ConsensusMsgType(consensusMessage.MsgType, len(signedSSVMessage.OperatorIDs))

	if err := mv.validateConsensusMessageSemantics(signedSSVMessage, consensusMessage, committeeInfo.operatorIDs); err != nil {
		return consensusMessage, err
	}

	state := mv.consensusState(signedSSVMessage.SSVMessage.GetID())

	if err := mv.validateQBFTLogic(signedSSVMessage, consensusMessage, committeeInfo.operatorIDs, receivedAt, state); err != nil {
		return consensusMessage, err
	}

	if err := mv.validateQBFTMessageByDutyLogic(signedSSVMessage, consensusMessage, committeeInfo.indices, receivedAt, state); err != nil {
		return consensusMessage, err
	}

	for i := range signedSSVMessage.Signatures {
		operatorID := signedSSVMessage.OperatorIDs[i]
		signature := signedSSVMessage.Signatures[i]

		if err := mv.signatureVerifier.VerifySignature(operatorID, ssvMessage, signature); err != nil {
			e := ErrSignatureVerification
			e.innerErr = fmt.Errorf("verify opid: %v signature: %w", operatorID, err)
			return consensusMessage, e
		}
	}

	if err := mv.updateConsensusState(signedSSVMessage, consensusMessage, state); err != nil {
		return consensusMessage, err
	}

	return consensusMessage, nil
}

func (mv *messageValidator) validateConsensusMessageSemantics(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	committee []spectypes.OperatorID,
) error {
	signers := signedSSVMessage.OperatorIDs
	quorumSize, _ := ssvtypes.ComputeQuorumAndPartialQuorum(len(committee))
	msgType := consensusMessage.MsgType

	if len(signers) > 1 {
		// Rule: Decided msg with different type than Commit
		if msgType != specqbft.CommitMsgType {
			e := ErrNonDecidedWithMultipleSigners
			e.got = len(signers)
			return e
		}

		// Rule: Number of signers must be >= quorum size
		if uint64(len(signers)) < quorumSize {
			e := ErrDecidedNotEnoughSigners
			e.want = quorumSize
			e.got = len(signers)
			return e
		}
	}

	if len(signedSSVMessage.FullData) != 0 {
		// Rule: Prepare or commit messages must not have full data
		if msgType == specqbft.PrepareMsgType || (msgType == specqbft.CommitMsgType && len(signers) == 1) {
			return ErrPrepareOrCommitWithFullData
		}

		hashedFullData, err := specqbft.HashDataRoot(signedSSVMessage.FullData)
		if err != nil {
			e := ErrFullDataHash
			e.innerErr = err
			return e
		}

		// Rule: Full data hash must match root
		if hashedFullData != consensusMessage.Root {
			return ErrInvalidHash
		}
	}

	// Rule: Consensus message type must be valid
	if !mv.validConsensusMsgType(msgType) {
		return ErrUnknownQBFTMessageType
	}

	// Rule: Round must not be zero
	if consensusMessage.Round == specqbft.NoRound {
		e := ErrZeroRound
		e.got = specqbft.NoRound
		return e
	}

	// Rule: consensus message must have the same identifier as the ssv message's identifier
	if !bytes.Equal(consensusMessage.Identifier, signedSSVMessage.SSVMessage.MsgID[:]) {
		e := ErrMismatchedIdentifier
		e.want = hex.EncodeToString(signedSSVMessage.SSVMessage.MsgID[:])
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
		// Rule: Signer must be the leader
		leader := mv.roundRobinProposer(consensusMessage.Height, consensusMessage.Round, committee)
		if signedSSVMessage.OperatorIDs[0] != leader {
			err := ErrSignerNotLeader
			err.got = signedSSVMessage.OperatorIDs[0]
			err.want = leader
			return err
		}
	}

	msgSlot := phase0.Slot(consensusMessage.Height)
	for _, signer := range signedSSVMessage.OperatorIDs {
		signerStateBySlot := state.GetOrCreate(signer)
		signerState := signerStateBySlot.Get(msgSlot)
		if signerState == nil {
			continue
		}

		if len(signedSSVMessage.OperatorIDs) == 1 {
			// Rule: Ignore if peer already advanced to a later round. Only for non-decided messages
			if consensusMessage.Round < signerState.Round {
				// Signers aren't allowed to decrease their round.
				// If they've sent a future message due to clock error,
				// they'd have to wait for the next slot/round to be accepted.
				err := ErrRoundAlreadyAdvanced
				err.want = signerState.Round
				err.got = consensusMessage.Round
				return err
			}

			if consensusMessage.Round == signerState.Round {
				// Rule: Peer must not send two proposals with different data
				if len(signedSSVMessage.FullData) != 0 && signerState.ProposalData != nil && !bytes.Equal(signerState.ProposalData, signedSSVMessage.FullData) {
					return ErrDifferentProposalData
				}

				// Rule: Peer must send only 1 proposal, 1 prepare, 1 commit, and 1 round-change per round
				limits := maxMessageCounts()
				if err := signerState.MessageCounts.ValidateConsensusMessage(signedSSVMessage, consensusMessage, limits); err != nil {
					return err
				}
			}
		} else if len(signedSSVMessage.OperatorIDs) > 1 {
			// Rule: Decided msg can't have the same signers as previously sent before for the same duty
			encodedOperators, err := encodeOperators(signedSSVMessage.OperatorIDs)
			if err != nil {
				return err
			}

			// Rule: Decided msg can't have the same signers as previously sent before for the same duty
			if _, ok := signerState.SeenSigners[encodedOperators]; ok {
				return ErrDecidedWithSameSigners
			}
		}
	}

	if len(signedSSVMessage.OperatorIDs) == 1 {
		// Rule: Round must not be smaller than current peer's round -1 or +1. Only for non-decided messages
		if err := mv.roundBelongsToAllowedSpread(signedSSVMessage, consensusMessage, receivedAt); err != nil {
			return err
		}
	}

	return nil
}

func (mv *messageValidator) validateQBFTMessageByDutyLogic(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	validatorIndices []phase0.ValidatorIndex,
	receivedAt time.Time,
	state *consensusState,
) error {
	// Rule: Height must not be "old". I.e., signer must not have already advanced to a later slot.
	if signedSSVMessage.SSVMessage.MsgID.GetRoleType() != spectypes.RoleCommittee { // Rule only for validator runners
		for _, signer := range signedSSVMessage.OperatorIDs {
			signerStateBySlot := state.GetOrCreate(signer)
			if maxSlot := signerStateBySlot.MaxSlot(); maxSlot > phase0.Slot(consensusMessage.Height) {
				e := ErrSlotAlreadyAdvanced
				e.got = consensusMessage.Height
				e.want = maxSlot
				return e
			}
		}
	}

	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()

	// Rule: Duty role has consensus (true except for ValidatorRegistration and VoluntaryExit)
	if role == spectypes.RoleValidatorRegistration || role == spectypes.RoleVoluntaryExit {
		e := ErrUnexpectedConsensusMessage
		e.got = role
		return e
	}

	msgSlot := phase0.Slot(consensusMessage.Height)
	if err := mv.validateBeaconDuty(signedSSVMessage.SSVMessage.GetID().GetRoleType(), msgSlot, validatorIndices); err != nil {
		return err
	}

	// Rule: current slot(height) must be between duty's starting slot and:
	// - duty's starting slot + 34 (committee and aggregation)
	// - duty's starting slot + 3 (other types)
	if err := mv.validateSlotTime(msgSlot, role, receivedAt); err != nil {
		return err
	}

	// Rule: valid number of duties per epoch:
	// - 2 for aggregation, voluntary exit and validator registration
	// - 2*V for Committee duty (where V is the number of validators in the cluster) (if no validator is doing sync committee in this epoch)
	// - else, accept
	for _, signer := range signedSSVMessage.OperatorIDs {
		signerStateBySlot := state.GetOrCreate(signer)
		if err := mv.validateDutyCount(signedSSVMessage.SSVMessage.GetID(), msgSlot, len(validatorIndices), signerStateBySlot); err != nil {
			return err
		}
	}

	// Rule: Round cut-offs for roles:
	// - 12 (committee and aggregation)
	// - 6 (other types)
	if maxRound := mv.maxRound(role); consensusMessage.Round > maxRound {
		err := ErrRoundTooHigh
		err.got = fmt.Sprintf("%v (%v role)", consensusMessage.Round, message.RunnerRoleToString(role))
		err.want = fmt.Sprintf("%v (%v role)", maxRound, message.RunnerRoleToString(role))
		return err
	}

	return nil
}

func (mv *messageValidator) updateConsensusState(signedSSVMessage *spectypes.SignedSSVMessage, consensusMessage *specqbft.Message, consensusState *consensusState) error {
	msgSlot := phase0.Slot(consensusMessage.Height)
	msgEpoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot)

	for _, signer := range signedSSVMessage.OperatorIDs {
		stateBySlot := consensusState.GetOrCreate(signer)
		signerState := stateBySlot.Get(msgSlot)
		if signerState == nil {
			signerState = NewSignerState(phase0.Slot(consensusMessage.Height), consensusMessage.Round)
			stateBySlot.Set(msgSlot, msgEpoch, signerState)
		} else {
			if consensusMessage.Round > signerState.Round {
				signerState.Reset(phase0.Slot(consensusMessage.Height), consensusMessage.Round)
			}
		}

		if err := mv.processSignerState(signedSSVMessage, consensusMessage, signerState); err != nil {
			return err
		}
	}

	return nil
}

func (mv *messageValidator) processSignerState(signedSSVMessage *spectypes.SignedSSVMessage, consensusMessage *specqbft.Message, signerState *SignerState) error {
	if len(signedSSVMessage.FullData) != 0 && consensusMessage.MsgType == specqbft.ProposalMsgType {
		signerState.ProposalData = signedSSVMessage.FullData
	}

	signerCount := len(signedSSVMessage.OperatorIDs)
	if signerCount > 1 {
		encodedOperators, err := encodeOperators(signedSSVMessage.OperatorIDs)
		if err != nil {
			// encodeOperators must never re
			return ErrEncodeOperators
		}

		signerState.SeenSigners[encodedOperators] = struct{}{}
	}

	signerState.MessageCounts.RecordConsensusMessage(signedSSVMessage, consensusMessage)
	return nil
}

func (mv *messageValidator) validateJustifications(message *specqbft.Message) error {
	pj, err := message.GetPrepareJustifications()
	if err != nil {
		e := ErrMalformedPrepareJustifications
		e.innerErr = err
		return e
	}

	// Rule: Can only exist for Proposal messages
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

	// Rule: Can only exist for Proposal or Round-Change messages
	if len(rcj) != 0 && message.MsgType != specqbft.ProposalMsgType && message.MsgType != specqbft.RoundChangeMsgType {
		e := ErrUnexpectedRoundChangeJustifications
		e.got = message.MsgType
		return e
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
	if currentQuickRound := specqbft.FirstRound + specqbft.Round(sinceSlotStart/roundtimer.QuickTimeout); currentQuickRound <= roundtimer.QuickTimeoutThreshold {
		return currentQuickRound
	}

	sinceFirstSlowRound := sinceSlotStart - (time.Duration(roundtimer.QuickTimeoutThreshold) * roundtimer.QuickTimeout)
	estimatedRound := roundtimer.QuickTimeoutThreshold + specqbft.FirstRound + specqbft.Round(sinceFirstSlowRound/roundtimer.SlowTimeout)
	return estimatedRound
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

	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()

	if consensusMessage.Round < lowestAllowed || consensusMessage.Round > highestAllowed {
		e := ErrEstimatedRoundNotInAllowedSpread
		e.got = fmt.Sprintf("%v (%v role)", consensusMessage.Round, message.RunnerRoleToString(role))
		e.want = fmt.Sprintf("between %v and %v (%v role) / %v passed", lowestAllowed, highestAllowed, message.RunnerRoleToString(role), sinceSlotStart)
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

func encodeOperators(operators []spectypes.OperatorID) ([sha256.Size]byte, error) {
	buf := new(bytes.Buffer)
	for _, operator := range operators {
		if err := binary.Write(buf, binary.LittleEndian, operator); err != nil {
			return [sha256.Size]byte{}, err
		}
	}
	hash := sha256.Sum256(buf.Bytes())
	return hash, nil
}
