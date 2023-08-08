package validation

// validator.go contains main code for validation and most of the rule checks.

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/networkconfig"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxMessageSize             = maxConsensusMsgSize
	maxConsensusMsgSize        = 8388608
	maxPartialSignatureMsgSize = 1952
	allowedRoundsInFuture      = 2
	allowedRoundsInPast        = 1
	lateSlotAllowance          = 2
)

type validationError struct {
	text string
	got  any
	want any
}

func (ve validationError) Error() string {
	result := ve.text
	if ve.got != nil {
		result += fmt.Sprintf(", got %v", ve.got)
	}
	if ve.want != nil {
		result += fmt.Sprintf(", want %v", ve.want)
	}

	return result
}

var (
	ErrEmptyData             = validationError{text: "empty data"}
	ErrDataTooBig            = validationError{text: "data too big", want: maxMessageSize}
	ErrUnknownValidator      = validationError{text: "unknown validator"}
	ErrInvalidRole           = validationError{text: "invalid role"}
	ErrEarlyMessage          = validationError{text: "early message"}
	ErrLateMessage           = validationError{text: "late message"}
	ErrNoSigners             = validationError{text: "no signers"}
	ErrSignerNotLeader       = validationError{text: "signer is not leader"}
	ErrWrongDomain           = validationError{text: "wrong domain"}
	ErrValidatorLiquidated   = validationError{text: "validator is liquidated"}
	ErrValidatorNotAttesting = validationError{text: "validator is not attesting"}
)

type ConsensusID struct {
	PubKey phase0.BLSPubKey
	Role   spectypes.BeaconRole
}

type ConsensusState struct {
	Signers map[spectypes.OperatorID]*SignerState
}

type MessageValidator struct {
	netCfg       networkconfig.NetworkConfig
	index        map[ConsensusID]*ConsensusState
	shareStorage registrystorage.Shares
}

func NewMessageValidator(netCfg networkconfig.NetworkConfig, shareStorage registrystorage.Shares) *MessageValidator {
	return &MessageValidator{
		netCfg:       netCfg,
		index:        make(map[ConsensusID]*ConsensusState),
		shareStorage: shareStorage,
	}
}

func (mv *MessageValidator) ValidateMessage(ssvMessage *spectypes.SSVMessage, receivedAt time.Time) (*queue.DecodedSSVMessage, error) {
	if len(ssvMessage.Data) == 0 {
		return nil, ErrEmptyData
	}

	if len(ssvMessage.Data) > maxMessageSize {
		err := ErrDataTooBig
		err.got = len(ssvMessage.Data)
		return nil, err
	}

	if !bytes.Equal(ssvMessage.MsgID.GetDomain(), mv.netCfg.Domain[:]) {
		err := ErrWrongDomain
		err.got = hex.EncodeToString(ssvMessage.MsgID.GetDomain())
		err.want = hex.EncodeToString(mv.netCfg.Domain[:])
		return nil, err
	}

	if !mv.validRole(ssvMessage.MsgID.GetRoleType()) {
		return nil, ErrInvalidRole
	}

	share := mv.shareStorage.Get(ssvMessage.MsgID.GetPubKey())
	if share == nil {
		return nil, ErrUnknownValidator
	}

	if share.Liquidated {
		return nil, ErrValidatorLiquidated
	}

	if !share.BeaconMetadata.IsAttesting() {
		return nil, ErrValidatorNotAttesting
	}

	msg, err := queue.DecodeSSVMessage(ssvMessage)
	if err != nil {
		return nil, fmt.Errorf("malformed message: %w", err)
	}

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		if err := mv.validateConsensusMessage(share, msg, receivedAt); err != nil {
			return nil, err
		}
	case spectypes.SSVPartialSignatureMsgType:
		if err := mv.validatePartialSignatureMessage(msg); err != nil {
			return nil, err
		}
	case ssvmessage.SSVEventMsgType:
		if err := mv.validateEventMessage(msg); err != nil {
			return nil, err
		}
	case spectypes.DKGMsgType: // TODO: handle
	}

	return msg, nil
}

func (mv *MessageValidator) validateConsensusMessage(share *ssvtypes.SSVShare, msg *queue.DecodedSSVMessage, receivedAt time.Time) error {
	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		return fmt.Errorf("expected consensus message")
	}

	if len(msg.Data) > maxConsensusMsgSize {
		return fmt.Errorf("size exceeded")
	}

	if len(signedMsg.Signature) != 96 {
		return fmt.Errorf("wrong signature size: %d", len(signedMsg.Signature))
	}

	// TODO: check if has running duty

	msgID := msg.GetID()

	// Validate that the specified signers belong in the validator's committee.
	if err := mv.validSigners(share, msg); err != nil {
		return err
	}

	messageSlot := phase0.Slot(signedMsg.Message.Height)

	if mv.earlyMessage(messageSlot, receivedAt) {
		return ErrEarlyMessage
	}
	if mv.lateMessage(messageSlot, msgID.GetRoleType(), receivedAt) { // TODO: use estimated slot here?
		return ErrLateMessage
	}

	if bytes.Equal(signedMsg.Signature, bytes.Repeat([]byte{0}, len(signedMsg.Signature))) {
		return fmt.Errorf("zero signature")
	}

	pj, err := signedMsg.Message.GetPrepareJustifications()
	if err != nil {
		return fmt.Errorf("malfrormed prepare justifications: %w", err)
	}

	rcj, err := signedMsg.Message.GetRoundChangeJustifications()
	if err != nil {
		return fmt.Errorf("malfrormed round change justifications: %w", err)
	}

	if err := ssvtypes.VerifyByOperators(signedMsg.Signature, signedMsg, mv.netCfg.Domain, spectypes.QBFTSignatureType, share.Committee); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	// TODO: checks for pj and rcj
	_ = pj
	_ = rcj

	consensusID := ConsensusID{
		PubKey: phase0.BLSPubKey(msgID.GetPubKey()),
		Role:   msgID.GetRoleType(),
	}
	// Validate each signer's behavior.
	state := mv.consensusState(consensusID)
	for _, signer := range signedMsg.Signers {
		if err := mv.validateSignerBehavior(state, signer, msg, receivedAt); err != nil {
			return fmt.Errorf("bad signed behavior: %w", err)
		}
	}

	return nil
}

func (mv *MessageValidator) validatePartialSignatureMessage(msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*spectypes.SignedPartialSignatureMessage)
	if !ok {
		return fmt.Errorf("expected partial signature message")
	}

	if len(msg.Data) > maxPartialSignatureMsgSize {
		return fmt.Errorf("size exceeded")
	}
	_ = signedMsg // TODO: validate it

	// TODO: check signature

	return nil
}
func (mv *MessageValidator) validateEventMessage(msg *queue.DecodedSSVMessage) error {
	_, ok := msg.Body.(*ssvtypes.EventMsg)
	if !ok {
		return fmt.Errorf("expected event message")
	}

	return fmt.Errorf("event messages are not broadcast")
}

func (mv *MessageValidator) earlyMessage(slot phase0.Slot, receivedAt time.Time) bool {
	return mv.netCfg.Beacon.GetSlotEndTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		Add(-clockErrorTolerance).Before(mv.netCfg.Beacon.GetSlotStartTime(slot))
}

func (mv *MessageValidator) lateMessage(slot phase0.Slot, role spectypes.BeaconRole, receivedAt time.Time) bool {
	// Note: this is just an example, should be according to ssv-spec.
	var ttl phase0.Slot
	switch role {
	case spectypes.BNRoleProposer, spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		ttl = 1 + lateSlotAllowance
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator:
		ttl = 32 + lateSlotAllowance
	case spectypes.BNRoleValidatorRegistration:
		ttl = 1
	}
	deadline := mv.netCfg.Beacon.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin).Add(clockErrorTolerance)
	return mv.netCfg.Beacon.GetSlotStartTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		After(deadline)
}

func (mv *MessageValidator) consensusState(id ConsensusID) *ConsensusState {
	if _, ok := mv.index[id]; !ok {
		mv.index[id] = &ConsensusState{
			Signers: make(map[spectypes.OperatorID]*SignerState),
		}
	}
	return mv.index[id]
}

func (mv *MessageValidator) validateSignerBehavior(
	state *ConsensusState,
	signer spectypes.OperatorID,
	msg *queue.DecodedSSVMessage,
	receivedAt time.Time,
) error {
	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		// TODO: add support
		return fmt.Errorf("not supported yet")
	}

	signerState := state.Signers[signer]
	if signerState == nil {
		signerState = &SignerState{}
	}

	// Validate slot.
	messageSlot := phase0.Slot(signedMsg.Message.Height)
	if signerState.Slot > messageSlot {
		// Signers aren't allowed to decrease their slot.
		// If they've sent a future message due to clock error,
		// this should be caught by the earlyMessage check.
		return errors.New("signer has already advanced to a later slot")
	}

	// Validate round.
	msgRound := signedMsg.Message.Round
	if signerState.Round > msgRound {
		// Signers aren't allowed to decrease their round.
		// If they've sent a future message due to clock error,
		// they'd have to wait for the next slot/round to be accepted.
		return errors.New("signer has already advanced to a later round")
	}

	if signerState.Slot < messageSlot && signerState.Round >= msgRound || signerState.Round < msgRound && signerState.Slot >= messageSlot {
		return errors.New("if slot is in future, round must be also in future and vice versa")
	}

	role := msg.MsgID.GetRoleType()
	maxRound := mv.maxRound(role)
	if msgRound > maxRound {
		return fmt.Errorf("round too high")
	}

	if msgRound-signerState.Round > allowedRoundsInFuture {
		return fmt.Errorf("round is too far in the future, current %d, got %d", signerState.Round, msgRound)
	}

	estimatedRound := mv.currentEstimatedRound(role, messageSlot, receivedAt)
	// msgRound - 2 <= estimatedRound <= msgRound + 1
	if estimatedRound-msgRound > allowedRoundsInFuture || msgRound-estimatedRound > allowedRoundsInPast {
		return fmt.Errorf("message round is too far from estimated, current %v, got %v", estimatedRound, msgRound)
	}

	if estimatedRound > maxRound {
		return fmt.Errorf("estimated round too high")
	}

	// Advance slot & round, if needed.
	if signerState.Slot < messageSlot || signerState.Round < msgRound {
		signerState.Reset(messageSlot, msgRound)
	}

	// TODO: if this is a round change message, we should somehow validate that it's not being sent too frequently.

	// TODO: move to MessageCounts?
	if signerState.ProposalData == nil {
		signerState.ProposalData = signedMsg.FullData
	} else if !bytes.Equal(signerState.ProposalData, signedMsg.FullData) {
		return fmt.Errorf("duplicated proposal with different data")
	}

	hasFullData := signedMsg.Message.MsgType == specqbft.ProposalMsgType ||
		signedMsg.Message.MsgType == specqbft.RoundChangeMsgType ||
		mv.isDecidedMessage(signedMsg)

	if hasFullData {
		hashedFullData, err := specqbft.HashDataRoot(signedMsg.FullData)
		if err != nil {
			return fmt.Errorf("hash data root: %w", err)
		}

		if hashedFullData != signedMsg.Message.Root {
			return fmt.Errorf("root doesn't match full data hash")
		}
	}

	if mv.isDecidedMessage(signedMsg) && len(signedMsg.Signers) <= signerState.LastDecidedQuorumSize {
		return fmt.Errorf("decided must have more signers than previous decided")
	}

	signerState.LastDecidedQuorumSize = len(signedMsg.Signers)

	if err := signerState.MessageCounts.Validate(msg); err != nil {
		return err
	}

	signerState.MessageCounts.Record(msg)

	// Validate message counts within the current round.
	if signerState.MessageCounts.Exceeds(maxMessageCounts(len(signedMsg.Signers))) {
		return errors.New("too many messages")
	}

	return nil
}

func (mv *MessageValidator) isDecidedMessage(signedMsg *specqbft.SignedMessage) bool {
	return signedMsg.Message.MsgType == specqbft.CommitMsgType && len(signedMsg.Signers) > 1
}

func (mv *MessageValidator) maxRound(role spectypes.BeaconRole) specqbft.Round {
	switch role {
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator:
		return 12 // TODO: consider calculating based on quick timeout and slow timeout
	case spectypes.BNRoleProposer, spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		return 6
	case spectypes.BNRoleValidatorRegistration:
		return 0
	default:
		panic("unknown role")
	}
}

// TODO: cover with tests
func (mv *MessageValidator) currentEstimatedRound(role spectypes.BeaconRole, slot phase0.Slot, receivedAt time.Time) specqbft.Round {
	slotStartTime := mv.netCfg.Beacon.GetSlotStartTime(slot)

	firstRoundStart := slotStartTime.Add(mv.waitAfterSlotStart(role))

	sinceFirstRound := receivedAt.Sub(firstRoundStart)
	if currentQuickRound := 1 + specqbft.Round(sinceFirstRound/roundtimer.QuickTimeout); currentQuickRound <= roundtimer.QuickTimeoutThreshold {
		return currentQuickRound
	}

	sinceFirstSlowRound := receivedAt.Sub(firstRoundStart.Add(time.Duration(roundtimer.QuickTimeoutThreshold) * roundtimer.QuickTimeout))
	return roundtimer.QuickTimeoutThreshold + 1 + specqbft.Round(sinceFirstSlowRound/roundtimer.SlowTimeout)
}

func (mv *MessageValidator) waitAfterSlotStart(role spectypes.BeaconRole) time.Duration {
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

func (mv *MessageValidator) validSigners(share *ssvtypes.SSVShare, msg *queue.DecodedSSVMessage) error {
	contains := func(signer spectypes.OperatorID) func(operator *spectypes.Operator) bool {
		return func(operator *spectypes.Operator) bool {
			return operator.OperatorID == signer
		}
	}

	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		if len(m.Signers) == 0 {
			return ErrNoSigners
		}

		if len(m.Signers) == 1 {
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
		} else if m.Message.MsgType != specqbft.CommitMsgType {
			return fmt.Errorf("non-decided with multiple signers, len: %d", len(m.Signers))
		} else if uint64(len(m.Signers)) < share.Quorum || len(m.Signers) > len(share.Committee) {
			return fmt.Errorf("decided signers size is not between partial quorum and quorum size")
		}

		if !slices.IsSorted(m.Signers) {
			return fmt.Errorf("signers not sorted")
		}

		// TODO: if decided, check if previous decided had less or equal signers

		seen := map[spectypes.OperatorID]struct{}{}
		for _, signer := range m.Signers {
			if signer == 0 {
				return fmt.Errorf("zero signer ID")
			}

			if _, ok := seen[signer]; ok {
				return fmt.Errorf("non-unique signer")
			}
			seen[signer] = struct{}{}

			if !slices.ContainsFunc(share.Committee, contains(signer)) {
				return fmt.Errorf("signer not in committee")
			}
		}

	case *spectypes.PartialSignatureMessage:
		if m.Signer == 0 {
			return fmt.Errorf("zero signer ID")
		}

		if !slices.ContainsFunc(share.Committee, contains(m.Signer)) {
			return fmt.Errorf("signer not in committee")
		}
	default:
		return fmt.Errorf("unknown message type: %T", m)
	}

	return nil
}

func (mv *MessageValidator) validRole(roleType spectypes.BeaconRole) bool {
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
