package validation

// consensus_validation.go contains methods for validating consensus messages

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/casts"
)

func (mv *messageValidator) validateConsensusMessage(
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	receivedFrom peer.ID,
	receivedAt time.Time,
) (*specqbft.Message, error) {
	ssvMessage := signedSSVMessage.SSVMessage

	if len(ssvMessage.Data) > maxEncodedConsensusMsgSize {
		e := ErrSSVDataTooBig
		e.got = len(ssvMessage.Data)
		e.want = maxEncodedConsensusMsgSize
		return nil, e
	}

	consensusMessage, err := specqbft.DecodeMessage(ssvMessage.Data)
	if err != nil {
		e := ErrUndecodableMessageData
		e.innerErr = err
		return nil, e
	}

	if err := mv.validateConsensusMessageSemantics(signedSSVMessage, consensusMessage, committeeInfo.committee); err != nil {
		return consensusMessage, err
	}

	key := peerIDWithMessageID{
		peerID:    receivedFrom,
		messageID: ssvMessage.GetID(),
	}
	state := mv.validatorState(key, committeeInfo.committee)

	if err := mv.validateQBFTLogic(signedSSVMessage, consensusMessage, committeeInfo, receivedAt, state); err != nil {
		return consensusMessage, err
	}

	if err := mv.validateQBFTMessageByDutyLogic(signedSSVMessage, consensusMessage, committeeInfo, receivedAt, state); err != nil {
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

	if err := mv.updateConsensusState(signedSSVMessage, consensusMessage, committeeInfo, state); err != nil {
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
	quorumSize, _ := ssvtypes.ComputeQuorumAndPartialQuorum(uint64(len(committee)))
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

	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()

	// Rule: Duty role has consensus (true except for ValidatorRegistration and VoluntaryExit)
	if role == spectypes.RoleValidatorRegistration || role == spectypes.RoleVoluntaryExit {
		e := ErrUnexpectedConsensusMessage
		e.got = role
		return e
	}

	// Rule: Round cut-offs for roles:
	// - 12 (committee and aggregation)
	// - 6 (other types)
	maxRound, err := mv.maxRound(role)
	if err != nil {
		return fmt.Errorf("failed to get max round: %w", err)
	}

	if consensusMessage.Round > maxRound {
		err := ErrRoundTooHigh
		err.got = fmt.Sprintf("%v (%v role)", consensusMessage.Round, message.RunnerRoleToString(role))
		err.want = fmt.Sprintf("%v (%v role)", maxRound, message.RunnerRoleToString(role))
		return err
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
	committeeInfo CommitteeInfo,
	receivedAt time.Time,
	state *ValidatorState,
) error {
	if consensusMessage.MsgType == specqbft.ProposalMsgType {
		// Rule: Signer must be the leader
		leader := mv.roundRobinProposer(consensusMessage.Height, consensusMessage.Round, committeeInfo.committee)
		if signedSSVMessage.OperatorIDs[0] != leader {
			err := ErrSignerNotLeader
			err.got = signedSSVMessage.OperatorIDs[0]
			err.want = leader
			return err
		}
	}

	msgSlot := phase0.Slot(consensusMessage.Height)
	for _, signer := range signedSSVMessage.OperatorIDs {
		signerStateBySlot := state.Signer(committeeInfo.signerIndex(signer))
		signerState := signerStateBySlot.GetSignerState(msgSlot)
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
				if len(signedSSVMessage.FullData) != 0 && signerState.HashedProposalData != nil {
					if *signerState.HashedProposalData != sha256.Sum256(signedSSVMessage.FullData) {
						return ErrDifferentProposalData
					}
				}

				// Rule: Peer must send only 1 proposal, 1 prepare, 1 commit, and 1 round-change per round
				if err := signerState.SeenMsgTypes.ValidateConsensusMessage(signedSSVMessage, consensusMessage); err != nil {
					return err
				}
			}
		} else if len(signedSSVMessage.OperatorIDs) > 1 {
			quorum := Quorum{
				Signers:   signedSSVMessage.OperatorIDs,
				Committee: committeeInfo.committee,
			}

			// Rule: Decided msg can't have the same signers as previously sent before for the same duty
			if signerState.SeenSigners != nil {
				if _, ok := signerState.SeenSigners[quorum.ToBitMask()]; ok {
					return ErrDecidedWithSameSigners
				}
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
	committeeInfo CommitteeInfo,
	receivedAt time.Time,
	state *ValidatorState,
) error {
	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()

	// Rule: Height must not be "old". I.e., signer must not have already advanced to a later slot.
	if role != spectypes.RoleCommittee { // Rule only for validator runners
		for _, signer := range signedSSVMessage.OperatorIDs {
			signerStateBySlot := state.Signer(committeeInfo.signerIndex(signer))
			if maxSlot := signerStateBySlot.MaxSlot(); maxSlot > phase0.Slot(consensusMessage.Height) {
				e := ErrSlotAlreadyAdvanced
				e.got = consensusMessage.Height
				e.want = maxSlot
				return e
			}
		}
	}

	msgSlot := phase0.Slot(consensusMessage.Height)
	randaoMsg := false
	if err := mv.validateBeaconDuty(signedSSVMessage.SSVMessage.GetID().GetRoleType(), msgSlot, committeeInfo.validatorIndices, randaoMsg); err != nil {
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
		signerStateBySlot := state.Signer(committeeInfo.signerIndex(signer))
		if err := mv.validateDutyCount(signedSSVMessage.SSVMessage.GetID(), msgSlot, committeeInfo.validatorIndices, signerStateBySlot); err != nil {
			return err
		}
	}

	return nil
}

func (mv *messageValidator) updateConsensusState(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	committeeInfo CommitteeInfo,
	consensusState *ValidatorState,
) error {
	msgSlot := phase0.Slot(consensusMessage.Height)
	msgEpoch := mv.netCfg.EstimatedEpochAtSlot(msgSlot)

	for _, signer := range signedSSVMessage.OperatorIDs {
		stateBySlot := consensusState.Signer(committeeInfo.signerIndex(signer))
		signerState := stateBySlot.GetSignerState(msgSlot)
		if signerState == nil {
			signerState = newSignerState(phase0.Slot(consensusMessage.Height), consensusMessage.Round)
			stateBySlot.SetSignerState(msgSlot, msgEpoch, signerState)
		} else {
			if consensusMessage.Round > signerState.Round {
				signerState.Reset(phase0.Slot(consensusMessage.Height), consensusMessage.Round)
			}
		}

		if err := mv.processSignerState(signedSSVMessage, consensusMessage, committeeInfo.committee, signerState); err != nil {
			return err
		}
	}

	return nil
}

func (mv *messageValidator) processSignerState(
	signedSSVMessage *spectypes.SignedSSVMessage,
	consensusMessage *specqbft.Message,
	committee []spectypes.OperatorID,
	signerState *SignerState,
) error {
	if len(signedSSVMessage.FullData) != 0 && consensusMessage.MsgType == specqbft.ProposalMsgType {
		fullDataHash := sha256.Sum256(signedSSVMessage.FullData)
		signerState.HashedProposalData = &fullDataHash
	}

	signerCount := len(signedSSVMessage.OperatorIDs)
	if signerCount > 1 {
		quorum := Quorum{
			Signers:   signedSSVMessage.OperatorIDs,
			Committee: committee,
		}

		if signerState.SeenSigners == nil {
			signerState.SeenSigners = make(map[SignersBitMask]struct{}) // lazy init on demand to reduce mem consumption
		}
		signerState.SeenSigners[quorum.ToBitMask()] = struct{}{}
	}

	return signerState.SeenMsgTypes.RecordConsensusMessage(signedSSVMessage, consensusMessage)
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

func (mv *messageValidator) maxRound(role spectypes.RunnerRole) (specqbft.Round, error) {
	switch role {
	case spectypes.RoleCommittee, spectypes.RoleAggregator: // TODO: check if value for aggregator is correct as there are messages on stage exceeding the limit
		return 12, nil // TODO: consider calculating based on quick timeout and slow timeout
	case spectypes.RoleProposer, spectypes.RoleSyncCommitteeContribution:
		return 6, nil
	default:
		return 0, fmt.Errorf("unknown role")
	}
}

func (mv *messageValidator) currentEstimatedRound(sinceSlotStart time.Duration) (specqbft.Round, error) {
	delta, err := casts.DurationToUint64(sinceSlotStart / roundtimer.QuickTimeout)
	if err != nil {
		return 0, fmt.Errorf("failed to convert time duration to uint64: %w", err)
	}
	if currentQuickRound := specqbft.FirstRound + specqbft.Round(delta); currentQuickRound <= roundtimer.QuickTimeoutThreshold {
		return currentQuickRound, nil
	}

	sinceFirstSlowRound := sinceSlotStart - (time.Duration(roundtimer.QuickTimeoutThreshold) * roundtimer.QuickTimeout)
	delta, err = casts.DurationToUint64(sinceFirstSlowRound / roundtimer.SlowTimeout)
	if err != nil {
		return 0, fmt.Errorf("failed to convert time duration to uint64: %w", err)
	}
	estimatedRound := roundtimer.QuickTimeoutThreshold + specqbft.FirstRound + specqbft.Round(delta)
	return estimatedRound, nil
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
	slotStartTime := mv.netCfg.GetSlotStartTime(phase0.Slot(consensusMessage.Height)) /*.
	Add(mv.waitAfterSlotStart(role))*/ // TODO: not supported yet because first round is non-deterministic now

	sinceSlotStart := time.Duration(0)
	estimatedRound := specqbft.FirstRound
	if receivedAt.After(slotStartTime) {
		sinceSlotStart = receivedAt.Sub(slotStartTime)
		currentEstimatedRound, err := mv.currentEstimatedRound(sinceSlotStart)
		if err != nil {
			return err
		}
		estimatedRound = currentEstimatedRound
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

func (mv *messageValidator) roundRobinProposer(height specqbft.Height, round specqbft.Round, committee []spectypes.OperatorID) spectypes.OperatorID {
	firstRoundIndex := uint64(0)
	if height != specqbft.FirstHeight {
		firstRoundIndex += uint64(height) % uint64(len(committee))
	}

	index := (firstRoundIndex + uint64(round) - uint64(specqbft.FirstRound)) % uint64(len(committee))
	return committee[index]
}
