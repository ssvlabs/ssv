package validation

import (
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50
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
}

func NewMessageValidator(network beaconprotocol.Network) *MessageValidator {
	return &MessageValidator{
		network: network,
		index:   make(map[ConsensusID]*ConsensusState),
	}
}

func (v *MessageValidator) ValidateMessage(ssvMessage *spectypes.SSVMessage) (*queue.DecodedSSVMessage, error) {
	if err := v.validateSSVMessage(ssvMessage); err != nil {
		return nil, err
	}

	msg, err := queue.DecodeSSVMessage(ssvMessage)
	if err != nil {
		return nil, fmt.Errorf("malformed message: %w", err)
	}

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		if err := v.validateConsensusMessage(msg); err != nil {
			return nil, err
		}
	case spectypes.SSVPartialSignatureMsgType:
		if err := v.validatePartialSignatureMessage(msg); err != nil {
			return nil, err
		}
	case ssvmessage.SSVEventMsgType:
		if err := v.validateEventMessage(msg); err != nil {
			return nil, err
		}
	}

	return msg, nil
}

func (v *MessageValidator) validateSSVMessage(msg *spectypes.SSVMessage) error {
	if !v.knownValidator(msg.MsgID.GetPubKey()) {
		// Validator doesn't exist or is liquidated.
		return ErrUnknownValidator
	}
	if !v.validRole(msg.MsgID.GetRoleType()) {
		return ErrInvalidRole
	}

	return nil
}

func (v *MessageValidator) validateConsensusMessage(msg *queue.DecodedSSVMessage) error {
	// TODO: consider having a slice of checks like this:
	//for _, check := range v.consensusChecks {
	//	if err := check(); err != nil {
	//		return err
	//	}
	//}

	signedMsg, ok := msg.Body.(*specqbft.SignedMessage)
	if !ok {
		return fmt.Errorf("expected consensus message")
	}

	msgID := msg.GetID()

	// Validate that the specified signers belong in the validator's committee.
	if !v.validSigners(msgID.GetPubKey(), msg) {
		return ErrInvalidSigners
	}

	// TODO: check if height can be used as slot
	slot := phase0.Slot(signedMsg.Message.Height)
	// Validate timing.
	if v.earlyMessage(slot) {
		return ErrEarlyMessage
	}
	if v.lateMessage(slot, msgID.GetRoleType()) {
		return ErrLateMessage
	}

	// TODO: consider using just msgID as it's an array and can be a key of a map
	consensusID := ConsensusID{
		PubKey: phase0.BLSPubKey(msgID.GetPubKey()),
		Role:   msgID.GetRoleType(),
	}
	// Validate each signer's behavior.
	state := v.consensusState(consensusID)
	for _, signer := range signedMsg.Signers {
		// TODO: should we verify signature before doing this validation?
		//    Reason being that this modifies the signer's state and so message fakers
		//    would be able to alter states of any signer using invalid signatures.
		if err := v.validateSignerBehavior(state, signer, msg); err != nil {
			return fmt.Errorf("bad signed behavior: %w", err)
		}
	}

	return nil
}

func (v *MessageValidator) validatePartialSignatureMessage(msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*spectypes.PartialSignatureMessage)
	if !ok {
		return fmt.Errorf("expected partial signature message")
	}

	_ = signedMsg // TODO: validate it

	return nil
}
func (v *MessageValidator) validateEventMessage(msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*ssvtypes.EventMsg)
	if !ok {
		return fmt.Errorf("expected event message")
	}

	_ = signedMsg // TODO: validate it

	return nil
}

func (v *MessageValidator) earlyMessage(slot phase0.Slot) bool {
	return v.network.GetSlotEndTime(v.network.EstimatedCurrentSlot()).
		Add(-clockErrorTolerance).Before(v.network.GetSlotStartTime(slot))
}

func (v *MessageValidator) lateMessage(slot phase0.Slot, role spectypes.BeaconRole) bool {
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
	deadline := v.network.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin + clockErrorTolerance)
	return v.network.GetSlotStartTime(v.network.EstimatedCurrentSlot()).
		After(deadline)
}

func (v *MessageValidator) consensusState(id ConsensusID) *ConsensusState {
	if _, ok := v.index[id]; !ok {
		v.index[id] = &ConsensusState{
			Signers: make(map[spectypes.OperatorID]*SignerState),
		}
	}
	return v.index[id]
}

func (v *MessageValidator) validateSignerBehavior(
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

func (v *MessageValidator) knownValidator(key []byte) bool {
	// TODO: implement
	return true
}

func (v *MessageValidator) validSigners(key []byte, msg *queue.DecodedSSVMessage) bool {
	// TODO: implement
	return true
}

func (v *MessageValidator) validRole(roleType spectypes.BeaconRole) bool {
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
