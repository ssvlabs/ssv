package queue

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

var (
	ErrUnknownMessageType = fmt.Errorf("unknown message type")
)

// DecodedSSVMessage is a bundle of SSVMessage and it's decoding.
// TODO: try to make it generic
type DecodedSSVMessage struct {
	// TODO: fork support
	// Fields are identical between the forks except for the role in MessageID
	// which can't contain attester and proposer roles atm.
	SignedSSVMessage *spectypes.SignedSSVMessage
	*spectypes.SSVMessage

	// Body is the decoded Data.
	Body interface{} // *genesisspecqbft.SignedMessage | *genesisspectypes.SignedPartialSignatureMessage | *EventMsg | *specqbft.Message | *spectypes.PartialSignatureMessages
}

// DecodeSSVMessage decodes an SSVMessage and returns a DecodedSSVMessage.
func DecodeSSVMessage(m *spectypes.SignedSSVMessage) (*DecodedSSVMessage, error) {
	var body interface{}
	switch m.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType: // TODO: Or message.SSVDecidedMsgType?
		sm := &genesisspecqbft.SignedMessage{}
		if err := sm.Decode(m.SSVMessage.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode Message")
		}
		body = sm
	case spectypes.SSVPartialSignatureMsgType:
		sm := &genesisspectypes.SignedPartialSignatureMessage{}
		if err := sm.Decode(m.SSVMessage.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode PartialSignatureMessages")
		}
		body = sm
	case ssvmessage.SSVEventMsgType:
		msg := &ssvtypes.EventMsg{}
		if err := msg.Decode(m.SSVMessage.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode EventMsg")
		}
		body = msg
	default:
		return nil, ErrUnknownMessageType
	}
	return &DecodedSSVMessage{
		SignedSSVMessage: m,
		SSVMessage:       m.SSVMessage,
		Body:             body,
	}, nil
}

// DecodeGenesisSSVMessage decodes a genesis SSVMessage and returns a DecodedSSVMessage.
func DecodeGenesisSSVMessage(m *genesisspectypes.SSVMessage) (*DecodedSSVMessage, error) {
	var body interface{}
	switch m.MsgType {
	case genesisspectypes.SSVConsensusMsgType: // TODO: Or message.SSVDecidedMsgType?
		sm := &genesisspecqbft.SignedMessage{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode SignedMessage")
		}
		body = sm
	case genesisspectypes.SSVPartialSignatureMsgType:
		sm := &genesisspectypes.SignedPartialSignatureMessage{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode SignedPartialSignatureMessage")
		}
		body = sm
	case genesisspectypes.MsgType(ssvmessage.SSVEventMsgType):
		msg := &ssvtypes.EventMsg{}
		if err := msg.Decode(m.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode EventMsg")
		}
		body = msg
	default:
		return nil, ErrUnknownMessageType
	}
	return &DecodedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.MsgType(m.MsgType),
			MsgID:   spectypes.MessageID(m.MsgID),
			Data:    m.Data,
		},
		Body: body,
	}, nil
}

// compareHeightOrSlot returns an integer comparing the message's height/slot to the current.
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func compareHeightOrSlot(state *State, m *DecodedSSVMessage) int {
	// TODO: fork support
	if mm, ok := m.Body.(*genesisspecqbft.SignedMessage); ok {
		if mm.Message.Height == genesisspecqbft.Height(state.Height) {
			return 0
		}
		if mm.Message.Height > genesisspecqbft.Height(state.Height) {
			return 1
		}
	} else if mm, ok := m.Body.(*genesisspectypes.SignedPartialSignatureMessage); ok {
		if mm.Message.Slot == state.Slot {
			return 0
		}
		if mm.Message.Slot > state.Slot {
			return 1
		}
	}
	return -1
}

// scoreRound returns an integer comparing the message's round (if exist) to the current.
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func scoreRound(state *State, m *DecodedSSVMessage) int {
	// TODO: fork support
	if mm, ok := m.Body.(*genesisspecqbft.SignedMessage); ok {
		if mm.Message.Round == genesisspecqbft.Round(state.Round) {
			return 2
		}
		if mm.Message.Round > genesisspecqbft.Round(state.Round) {
			return 1
		}
		return -1
	}
	return 0
}

// scoreMessageType returns a score based on the top level message type,
// where event type messages are prioritized over other types.
func scoreMessageType(m *DecodedSSVMessage) int {
	switch mm := m.Body.(type) {
	case *ssvtypes.EventMsg:
		switch mm.Type {
		case ssvtypes.ExecuteDuty:
			return 3
		case ssvtypes.Timeout:
			return 2
		}
		return 0
	default:
		return 0
	}
}

// scoreMessageSubtype returns an integer score for the message's type.
func scoreMessageSubtype(state *State, m *DecodedSSVMessage, relativeHeight int) int {
	consensusMessage, isConsensusMessage := m.Body.(*genesisspecqbft.SignedMessage)

	var (
		isPreConsensusMessage  = false
		isPostConsensusMessage = false
	)
	if mm, ok := m.Body.(*genesisspectypes.SignedPartialSignatureMessage); ok {
		isPostConsensusMessage = mm.Message.Type == genesisspectypes.PostConsensusPartialSig
		isPreConsensusMessage = !isPostConsensusMessage
	}

	// Current height.
	if relativeHeight == 0 {
		if state.HasRunningInstance {
			switch {
			case isConsensusMessage:
				return 3
			case isPreConsensusMessage:
				return 2
			case isPostConsensusMessage:
				return 1
			}
			return 0
		}
		switch {
		case isPreConsensusMessage:
			return 3
		case isPostConsensusMessage:
			return 2
		case isConsensusMessage:
			return 1
		}
		return 0
	}

	// Higher height.
	if relativeHeight == 1 {
		switch {
		case isDecidedMesssage(state, consensusMessage):
			return 4
		case isPreConsensusMessage:
			return 3
		case isConsensusMessage:
			return 2
		case isPostConsensusMessage:
			return 1
		}
		return 0
	}

	// Lower height.
	switch {
	case isDecidedMesssage(state, consensusMessage):
		return 2
	case isConsensusMessage && consensusMessage.Message.MsgType == genesisspecqbft.CommitMsgType:
		return 1
	}
	return 0
}

// scoreConsensusType returns an integer score for the type of a consensus message.
// When given a non-consensus message, scoreConsensusType returns 0.
func scoreConsensusType(state *State, m *DecodedSSVMessage) int {
	if mm, ok := m.Body.(*genesisspecqbft.SignedMessage); ok {
		switch mm.Message.MsgType {
		case genesisspecqbft.ProposalMsgType:
			return 4
		case genesisspecqbft.PrepareMsgType:
			return 3
		case genesisspecqbft.CommitMsgType:
			return 2
		case genesisspecqbft.RoundChangeMsgType:
			return 1
		}
	}
	return 0
}

func isDecidedMesssage(s *State, sm *genesisspecqbft.SignedMessage) bool {
	if sm == nil {
		return false
	}
	return sm.Message.MsgType == genesisspecqbft.CommitMsgType &&
		len(sm.Signers) > int(s.Quorum)
}
