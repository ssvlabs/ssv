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

	maxMessageSize             = 8388608        // TODO: define
	maxConsensusMsgSize        = maxMessageSize // TODO: define
	maxPartialSignatureMsgSize = maxMessageSize // TODO: define
	allowedRoundsInFuture      = 2
)

var (
	ErrUnknownValidator = errors.New("unknown validator")
	ErrInvalidRole      = errors.New("invalid role")
	ErrEarlyMessage     = errors.New("early message")
	ErrLateMessage      = errors.New("late message")
	ErrInvalidSigners   = errors.New("invalid signers")
	ErrNoSigners        = errors.New("no signers")
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

func (mv *MessageValidator) ValidateMessage(ssvMessage *spectypes.SSVMessage) (*queue.DecodedSSVMessage, error) {
	if len(ssvMessage.Data) > maxMessageSize {
		return nil, fmt.Errorf("message to big, got %d bytes, limit %d bytes", len(ssvMessage.Data), maxMessageSize)
	}

	if !bytes.Equal(ssvMessage.MsgID.GetDomain(), mv.netCfg.Domain[:]) {
		return nil, fmt.Errorf("wrong domain, got %v, expected %v", hex.EncodeToString(ssvMessage.MsgID.GetDomain()), hex.EncodeToString(mv.netCfg.Domain[:]))
	}

	share := mv.shareStorage.Get(ssvMessage.MsgID.GetPubKey())
	if share != nil {
		return nil, ErrUnknownValidator // ERR_VALIDATOR_ID_MISMATCH
	}

	if !share.BeaconMetadata.IsAttesting() {
		return nil, fmt.Errorf("validator is not active")
	}

	if !mv.validRole(ssvMessage.MsgID.GetRoleType()) {
		return nil, ErrInvalidRole
	}

	if len(ssvMessage.Data) == 0 {
		return nil, fmt.Errorf("empty data") // ERR_NO_DATA
	}

	msg, err := queue.DecodeSSVMessage(ssvMessage)
	if err != nil {
		return nil, fmt.Errorf("malformed message: %w", err)
	}

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		if err := mv.validateConsensusMessage(share, msg); err != nil {
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

func (mv *MessageValidator) validateConsensusMessage(share *ssvtypes.SSVShare, msg *queue.DecodedSSVMessage) error {
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

	msgID := msg.GetID()

	// Validate that the specified signers belong in the validator's committee.
	if err := mv.validSigners(share, msg); err != nil {
		return err
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

	if bytes.Equal(signedMsg.Signature, bytes.Repeat([]byte{0}, len(signedMsg.Signature))) {
		return fmt.Errorf("zero signature")
	}

	// verify signature
	if err := ssvtypes.VerifyByOperators(signedMsg.Signature, signedMsg, mv.netCfg.Domain, spectypes.QBFTSignatureType, share.Committee); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

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

	// TODO: check signature

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
	return mv.netCfg.Beacon.GetSlotEndTime(mv.netCfg.Beacon.EstimatedCurrentSlot()).
		Add(-clockErrorTolerance).Before(mv.netCfg.Beacon.GetSlotStartTime(slot))
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
	deadline := mv.netCfg.Beacon.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin + clockErrorTolerance)
	return mv.netCfg.Beacon.GetSlotStartTime(mv.netCfg.Beacon.EstimatedCurrentSlot()).
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

	if signerState.Slot < messageSlot && signerState.Round >= msgRound || signerState.Round < msgRound && signerState.Slot >= messageSlot {
		return errors.New("if slot is in future, round must be also in future and vice versa")
	}

	if msgRound-signerState.Round > allowedRoundsInFuture {
		return fmt.Errorf("round is too far in the future, current %d, got %d", signerState.Round, msgRound)
	}

	// TODO: check that the message's round is sensible according to the roundTimeout
	// and the slot. For example:
	//
	//     maxRound := (currentTime-messageSlotTime)/roundTimeout
	//     assert msg.Round < maxRound

	role := msg.MsgID.GetRoleType()
	maxRound := mv.maxRound(role)
	if msgRound > maxRound {
		return fmt.Errorf("round too high")
	}

	estimatedRound := mv.currentEstimatedRound(role, phase0.Slot(signedMsg.Message.Height))
	// msgRound - 2 <= estimatedRound <= msgRound + 1
	if estimatedRound-msgRound > 2 || msgRound-estimatedRound > 1 {
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

func (mv *MessageValidator) maxRound(role spectypes.BeaconRole) specqbft.Round {
	switch role {
	case spectypes.BNRoleAttester:
		return 12 // TODO: consider calculating based on quick timeout and slow timeout
	// TODO: other roles
	default:
		panic("unknown role")
	}
}

func (mv *MessageValidator) currentEstimatedRound(role spectypes.BeaconRole, slot phase0.Slot) specqbft.Round {
	slotStartTime := mv.netCfg.Beacon.GetSlotStartTime(slot)

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
		return mv.netCfg.Beacon.SlotDurationSec() / 3
	// TODO: other roles
	default:
		panic("unknown role")
	}
}

func (mv *MessageValidator) validSigners(share *ssvtypes.SSVShare, msg *queue.DecodedSSVMessage) error {
	// TODO: consider having different validation functions for each message type
	// TODO: consider returning error
	if !share.BeaconMetadata.IsAttesting() {
		return fmt.Errorf("validator not active")
	}

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

		if len(m.Signers) > 1 && m.Message.MsgType != specqbft.CommitMsgType {
			return fmt.Errorf("non-decided with multiple signers, len: %d", len(m.Signers))
		}

		if !slices.IsSorted(m.Signers) {
			return fmt.Errorf("signers not sorted") // ERR_SIGNERS_NOT_SORTED
		}

		// TODO: if decided, check if previous decided had less or equal signers

		seen := map[spectypes.OperatorID]struct{}{}
		for _, signer := range m.Signers {
			if signer == 0 {
				return fmt.Errorf("zero signer ID") // ERR_SIG_ID
			}

			if _, ok := seen[signer]; ok {
				return fmt.Errorf("non-unique signer") // ERR_NON_UNIQUE_SIG
			}
			seen[signer] = struct{}{}

			if !slices.ContainsFunc(share.Committee, contains(signer)) {
				return fmt.Errorf("signer not in committee") // ERR_SIG_ID
			}
		}
	case *spectypes.PartialSignatureMessage:
		if m.Signer == 0 {
			return fmt.Errorf("zero signer ID") // ERR_SIG_ID
		}

		if !slices.ContainsFunc(share.Committee, contains(m.Signer)) {
			return fmt.Errorf("signer not in committee") // ERR_SIG_ID
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
