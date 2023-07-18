## Brief
Today, SSV nodes perform most of the validations on messages only in the consensus flow.

## Purpose
Shield honest nodes from wasteful compute and I/O resource consumption by malicious/faulty nodes.

## Goals
- [x] Completeness — Validate *all* incoming messages before reacting on them
    - Syntactic: is the message well-formed?
    - Semantic: is the message playing by the protocol's rules?
    - Cryptographic: is the signature correct?
- [x] Accountability — Penalize peers who transmit invalid messages
- [x] Efficiency — BLS signature verifications are heavy!
    - Only verify signatures for messages which passed the synactic & semantic validations.
    - Batch BLS signature verifications to reduce CPU usage

## Pseudocode
The following is a pseudocode demonstrating how to perform the syntactic & semantic validations on messages.
```go
package validation

import (
	"errors"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50
)

var (
	ErrUnknownValidator  = errors.New("unknown validator")
	ErrInvalidRole       = errors.New("invalid role")
	ErrMalformedMessage  = errors.New("malformed message")
	ErrEarlyMessage      = errors.New("early message")
	ErrLateMessage       = errors.New("late message")
	ErrInvalidSigners    = errors.New("invalid signers")
	ErrBadSignerBehavior = errors.New("bad signer behavior")
)

// maxMessageCounts is the maximum number of acceptable messages from a signer within a slot & round.
func maxMessageCounts(committeeSize uint64) MessageCounts {
	return MessageCounts{
		PreConsensus:  1,
		Proposals:     1,
		Prepares:      1,
        // TODO: max commits should adapt to the committeeSize.
        //     (see Gal's formula: https://hackmd.io/zT1hct3oRDW3QicByFkqsA)
		Commits:       0,
		PostConsensus: 1,
	}
}

type ConsensusID struct {
	PubKey phase0.BLSPubKey
	Role   spectypes.BeaconRole
}

type ConsensusState struct {
	Signers map[spectypes.OperatorID]*SignerState
}

type MessageCounts struct {
	PreConsensus  int
	Proposals     int
	Prepares      int
	Commits       int
	PostConsensus int
}

func (c *MessageCounts) Record(msg *queue.DecodedSSVMessage, limits MessageCounts) bool {
	switch m := msg.Body.(type) {
	case *specqbft.SignedMessage:
		switch m.Message.MsgType {
		case specqbft.ProposalMsgType:
			c.Proposals++
		case specqbft.PrepareMsgType:
			c.Prepares++
		case specqbft.CommitMsgType:
			c.Commits++
		}
	case *spectypes.SignedPartialSignatureMessage:
		if m.Message.Type == spectypes.PostConsensusPartialSig {
			c.PostConsensus++
		} else {
			c.PreConsensus++
		}
	}
	return c.Exceeds()
}

func (c *MessageCounts) Exceeds(limits MessageCounts) bool {
	return c.PreConsensus > limits.PreConsensus ||
		c.Proposals > limits.Proposals ||
		c.Prepares > limits.Prepares ||
		c.Commits > limits.Commits ||
		c.PostConsensus > limits.PostConsensus
}

type SignerState struct {
	Start         time.Time
	Slot          phase0.Slot
	Round         specqbft.Round
	MessageCounts MessageCounts
}

func (s *SignerState) Reset(slot phase0.Slot, round specqbft.Round) {
	s.Start = time.Now()
	s.Slot = slot
	s.MessageCounts = MessageCounts{}
}

type MessageValidator struct {
	network beaconprotocol.Network
	index   map[ConsensusID]*ConsensusState
}

func (v *MessageValidator) ValidateMessage(ssvMessage *spectypes.SSVMessage) (*queue.DecodedSSVMessage, error) {
	// Pre-decode validations.
	id := ConsensusID{
		PubKey: ssvMessage.MsgID.GetPubKey(),
		Role:   ssvMessage.MsgID.GetRoleType(),
	}
	if !v.knownValidator(id.PubKey) {
		// Validator doesn't exist or is liquidated.
		return nil, ErrUnknownValidator
	}
	if !v.validRole(id.Role) {
		return nil, ErrInvalidRole
	}

	// Decode.
	msg, err := queue.DecodeSSVMessage(ssvMessage)
	if err != nil {
		return nil, errors.Join(ErrMalformedMessage, err)
	}

	// Validate that the specified signers belong in the validator's committee.
	if !v.validSigners(id.PubKey, msg) {
		return nil, ErrInvalidSigners
	}

	// Validate timing.
	if v.earlyMessage(msg.Slot) {
		return nil, ErrEarlyMessage
	}
	if v.lateMessage(msg.Slot, id.Role) {
		return nil, ErrLateMessage
	}

	// Validate each signer's behavior.
	state := v.consensusState(id)
	for _, signer := range msg.Signers {
        // TODO: should we verify signature before doing this validation?
        //    Reason being that this modifies the signer's state and so message fakers
        //    would be able to alter states of any signer using invalid signatures.
		if err := v.validateSignerBehavior(state, signer, msg); err != nil {
			return nil, errors.Join(ErrBadSignerBehavior, err)
		}
	}

	return msg, nil
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
	state ConsensusState,
	signer spectypes.OperatorID,
	msg *queue.DecodedSSVMessage,
) error {
	signerState := state.Signers[signer]
	if signerState == nil {
		signerState = &SignerState{}
	}

	// Validate slot.
	if signerState.Slot > msg.Slot {
		// Signers aren't allowed to decrease their slot.
		// If they've sent a future message due to clock error,
		// this should be caught by the earlyMessage check.
		return errors.New("signer has already advanced to a later slot")
	}

	// Validate round.
	var msgRound specqbft.Round
	if m, ok := msg.Body.(*specqbft.SignedMessage); ok {
		msgRound = m.Message.Round
	}
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
	if signerState.Slot < msg.Slot || signerState.Round < msgRound {
		signerState.Reset(msg.Slot, msgRound)
	}

	// TODO: if this is a round change message, we should somehow validate that it's not being sent too frequently.

	// Validate message counts within the current round.
	if !signerState.MessageCounts.Record(msg, maxMessageCounts) {
		return errors.New("too many messages")
	}

	return nil
}
```