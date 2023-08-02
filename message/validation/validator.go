package validation

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/operator/validator"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxConsensusMsgSize        = math.MaxUint64 // TODO: define
	maxPartialSignatureMsgSize = math.MaxUint64 // TODO: define
)

var (
	ErrUnknownValidator = errors.New("unknown validator")
	ErrInvalidRole      = errors.New("invalid role")
	ErrEarlyMessage     = errors.New("early message")
	ErrLateMessage      = errors.New("late message")
	ErrInvalidSigners   = errors.New("invalid signers")
)

type ConsensusID struct {
	PubKey phase0.BLSPubKey
	Role   spectypes.BeaconRole
}

type ConsensusState struct {
	Signers map[spectypes.OperatorID]*SignerState
}

type MessageValidator struct {
	network beaconprotocol.Network
	index   map[ConsensusID]*ConsensusState
	ctrl    validator.Controller
}

func NewMessageValidator(network beaconprotocol.Network, ctrl validator.Controller) *MessageValidator {
	return &MessageValidator{
		network: network,
		index:   make(map[ConsensusID]*ConsensusState),
		ctrl:    ctrl,
	}
}

func (mv *MessageValidator) ValidateMessage(ssvMessage *spectypes.SSVMessage) (*queue.DecodedSSVMessage, error) {
	if err := mv.validateSSVMessage(ssvMessage); err != nil {
		return nil, err
	}

	msg, err := queue.DecodeSSVMessage(ssvMessage)
	if err != nil {
		return nil, fmt.Errorf("malformed message: %w", err)
	}

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		if err := mv.validateConsensusMessage(msg); err != nil {
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
	}

	return msg, nil
}

func (mv *MessageValidator) validateSSVMessage(msg *spectypes.SSVMessage) error {
	// TODO: check domain
	if !mv.knownValidator(msg.MsgID.GetPubKey()) {
		// Validator doesn't exist or is liquidated.
		return ErrUnknownValidator // ERR_VALIDATOR_ID_MISMATCH
	}
	if !mv.validRole(msg.MsgID.GetRoleType()) {
		return ErrInvalidRole
	}

	if len(msg.Data) == 0 {
		return fmt.Errorf("empty data") // ERR_NO_DATA
	}

	return nil
}

func (mv *MessageValidator) validateConsensusMessage(msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		return fmt.Errorf("expected consensus message")
	}

	if len(msg.Data) > maxPartialSignatureMsgSize {
		return fmt.Errorf("size exceeded")
	}

	msgID := msg.GetID()

	// Validate that the specified signers belong in the validator's committee.
	if !mv.validSigners(msgID.GetPubKey(), msg) {
		return ErrInvalidSigners
	}

	// TODO: check if height can be used as slot
	slot := phase0.Slot(signedMsg.Message.Height)
	// Validate timing.
	if mv.earlyMessage(slot) {
		return ErrEarlyMessage
	}
	if mv.lateMessage(slot, msgID.GetRoleType()) {
		return ErrLateMessage
	}

	// TODO: consider using just msgID as it's an array and can be a key of a map
	consensusID := ConsensusID{
		PubKey: phase0.BLSPubKey(msgID.GetPubKey()),
		Role:   msgID.GetRoleType(),
	}
	// Validate each signer's behavior.
	state := mv.consensusState(consensusID)
	for _, signer := range signedMsg.Signers {
		// TODO: should we verify signature before doing this validation?
		//    Reason being that this modifies the signer's state and so message fakers
		//    would be able to alter states of any signer using invalid signatures.
		if err := mv.validateSignerBehavior(state, signer, msg); err != nil {
			return fmt.Errorf("bad signed behavior: %w", err)
		}
	}

	return nil
}

func (mv *MessageValidator) validatePartialSignatureMessage(msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*spectypes.PartialSignatureMessage)
	if !ok {
		return fmt.Errorf("expected partial signature message")
	}

	if len(msg.Data) > maxPartialSignatureMsgSize {
		return fmt.Errorf("size exceeded") // ERR_PSIG_MSG_SIZE
	}
	_ = signedMsg // TODO: validate it

	return nil
}
func (mv *MessageValidator) validateEventMessage(msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*ssvtypes.EventMsg)
	if !ok {
		return fmt.Errorf("expected event message")
	}

	_ = signedMsg // TODO: validate it

	return nil
}

func (mv *MessageValidator) earlyMessage(slot phase0.Slot) bool {
	return mv.network.GetSlotEndTime(mv.network.EstimatedCurrentSlot()).
		Add(-clockErrorTolerance).Before(mv.network.GetSlotStartTime(slot))
}

func (mv *MessageValidator) lateMessage(slot phase0.Slot, role spectypes.BeaconRole) bool {
	// Note: this is just an example, should be according to ssv-spec.
	var ttl phase0.Slot
	switch role {
	case spectypes.BNRoleProposer:
		ttl = 1
	case spectypes.BNRoleAttester:
		ttl = 32
	case spectypes.BNRoleSyncCommittee:
		ttl = 1
		// ...
	}
	deadline := mv.network.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin + clockErrorTolerance)
	return mv.network.GetSlotStartTime(mv.network.EstimatedCurrentSlot()).
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
	// TODO: check if height can be used as slot
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

	// TODO: check that the message's round is sensible according to the roundTimeout
	// and the slot. For example:
	//
	//     maxRound := (currentTime-messageSlotTime)/roundTimeout
	//     assert msg.Round < maxRound

	role := msg.MsgID.GetRoleType()
	maxRound := mv.maxRound(role)
	if signedMsg.Message.Round > maxRound {
		return fmt.Errorf("round too high")
	}

	estimatedRound := mv.currentEstimatedRound(role, phase0.Slot(signedMsg.Message.Height)) // TODO: check if height == slot
	if signedMsg.Message.Round != estimatedRound {
		return fmt.Errorf("message round differs from estimated, want %v, got %v", estimatedRound, signedMsg.Message.Round)
	}

	if estimatedRound > maxRound {
		return fmt.Errorf("estimated round too high")
	}

	// TODO: check current estimated round

	// Advance slot & round, if needed.
	if signerState.Slot < messageSlot || signerState.Round < msgRound {
		signerState.Reset(messageSlot, msgRound)
	}

	// TODO: if this is a round change message, we should somehow validate that it's not being sent too frequently.

	// Validate message counts within the current round.
	if !signerState.MessageCounts.Record(msg, maxMessageCounts(len(signedMsg.Signers))) {
		return errors.New("too many messages")
	}

	return nil
}

func (mv *MessageValidator) maxRound(role spectypes.BeaconRole) specqbft.Round {
	switch role {
	case spectypes.BNRoleAttester:
		return 12
	// TODO: other roles
	default:
		panic("unknown role")
	}
}

func (mv *MessageValidator) currentEstimatedRound(role spectypes.BeaconRole, slot phase0.Slot) specqbft.Round {
	slotStartTime := mv.network.GetSlotStartTime(slot)

	firstRoundStart := slotStartTime.Add(mv.waitAfterSlotStart(role))
	messageTime := time.Now() // TODO: attach it to the message when it's received

	sinceFirstRound := messageTime.Sub(firstRoundStart)
	if currentQuickRound := 1 + specqbft.Round(sinceFirstRound/roundtimer.QuickTimeout); currentQuickRound <= roundtimer.QuickTimeoutThreshold {
		return currentQuickRound
	}

	sinceFirstSlowRound := messageTime.Sub(firstRoundStart.Add(time.Duration(roundtimer.QuickTimeoutThreshold) * roundtimer.QuickTimeout))
	return roundtimer.QuickTimeoutThreshold + 1 + specqbft.Round(sinceFirstSlowRound/roundtimer.SlowTimeout)
}

func (mv *MessageValidator) waitAfterSlotStart(role spectypes.BeaconRole) time.Duration {
	switch role {
	case spectypes.BNRoleAttester:
		return mv.network.SlotDurationSec() / 3
	// TODO: other roles
	default:
		panic("unknown role")
	}
}

func (mv *MessageValidator) knownValidator(pubKey []byte) bool {
	_, known := mv.ctrl.GetValidator(hex.EncodeToString(pubKey))
	return known
}

func (mv *MessageValidator) validSigners(pubKey []byte, msg *queue.DecodedSSVMessage) bool {
	// TODO: consider having different validation functions for each message type
	// TODO: consider returning error

	v, known := mv.ctrl.GetValidator(hex.EncodeToString(pubKey))
	if !known || mv == nil {
		return false
	}

	contains := func(signer spectypes.OperatorID) func(operator *spectypes.Operator) bool {
		return func(operator *spectypes.Operator) bool {
			return operator.OperatorID == signer
		}
	}

	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		if len(m.Signers) == 0 {
			return false // ERR_NO_SIG
		}

		if !slices.IsSorted(m.Signers) {
			return false // ERR_SIGNERS_NOT_SORTED
		}

		seen := map[spectypes.OperatorID]struct{}{}
		for _, signer := range m.Signers {
			if signer == 0 {
				return false // ERR_SIG_ID
			}

			if _, ok := seen[signer]; ok {
				return false // ERR_NON_UNIQUE_SIG
			}
			seen[signer] = struct{}{}

			if !slices.ContainsFunc(v.Share.Committee, contains(signer)) {
				return false // ERR_SIG_ID
			}
		}
	case *spectypes.PartialSignatureMessage:
		if m.Signer == 0 {
			return false // ERR_SIG_ID
		}

		if !slices.ContainsFunc(v.Share.Committee, contains(m.Signer)) {
			return false // ERR_SIG_ID
		}
	default:
		return false
	}

	return true
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
