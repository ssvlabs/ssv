package queue

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/network/commons"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

var (
	ErrUnknownMessageType = fmt.Errorf("unknown message type")
	ErrDecodeNetworkMsg   = fmt.Errorf("could not decode data into an SSVMessage")
)

// DecodedSSVMessage is a bundle of SSVMessage and it's decoding.
// TODO: try to make it generic
type DecodedSSVMessage struct {
	*spectypes.SignedSSVMessage
	*spectypes.SSVMessage

	// Body is the decoded Data.
	Body interface{} // *specqbft.Message | *spectypes.PartialSignatureMessages | *EventMsg
}

// DecodeSSVMessage decodes a SSVMessage into a DecodedSSVMessage.
func DecodeSSVMessage(m *spectypes.SSVMessage) (*DecodedSSVMessage, error) {
	body, err := ExtractMsgBody(m)
	if err != nil {
		return nil, err
	}

	return &DecodedSSVMessage{
		SSVMessage: m,
		Body:       body,
	}, nil
}

// DecodeSignedSSVMessage decodes a SignedSSVMessage into a DecodedSSVMessage.
func DecodeSignedSSVMessage(sm *spectypes.SignedSSVMessage) (*DecodedSSVMessage, error) {
	m, err := commons.DecodeNetworkMsg(sm.SSVMessage.Data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecodeNetworkMsg, err)
	}

	d, err := DecodeSSVMessage(m)
	if err != nil {
		return nil, err
	}

	d.SignedSSVMessage = sm

	return d, nil
}

func ExtractMsgBody(m *spectypes.SSVMessage) (interface{}, error) {
	var body interface{}
	switch m.MsgType {
	case spectypes.SSVConsensusMsgType: // TODO: Or message.SSVDecidedMsgType?
		sm := &specqbft.Message{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, fmt.Errorf("failed to decode SignedMessage: %w", err)
		}
		body = sm
	case spectypes.SSVPartialSignatureMsgType:
		sm := &spectypes.PartialSignatureMessages{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, fmt.Errorf("failed to decode SignedPartialSignatureMessage: %w", err)
		}
		body = sm
	case ssvmessage.SSVEventMsgType:
		msg := &ssvtypes.EventMsg{}
		if err := msg.Decode(m.Data); err != nil {
			return nil, fmt.Errorf("failed to decode EventMsg: %w", err)
		}
		body = msg
	default:
		return nil, ErrUnknownMessageType
	}

	return body, nil
}

// compareHeightOrSlot returns an integer comparing the message's height/slot to the current.
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func compareHeightOrSlot(state *State, m *DecodedSSVMessage) int {
	if qbftMsg, ok := m.Body.(*specqbft.Message); ok {
		if qbftMsg.Height == state.Height {
			return 0
		}
		if qbftMsg.Height > state.Height {
			return 1
		}
	} else if pms, ok := m.Body.(*spectypes.PartialSignatureMessages); ok { // everyone likes pms
		if pms.Slot == state.Slot {
			return 0
		}
		if pms.Slot > state.Slot {
			return 1
		}
	}
	return -1
}

// scoreRound returns an integer comparing the message's round (if exist) to the current.
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func scoreRound(state *State, m *DecodedSSVMessage) int {
	if qbftMsg, ok := m.Body.(*specqbft.Message); ok {
		if qbftMsg.Round == state.Round {
			return 2
		}
		if qbftMsg.Round > state.Round {
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
	_, isConsensusMessage := m.Body.(*specqbft.Message)

	var (
		isPreConsensusMessage  = false
		isPostConsensusMessage = false
	)
	if mm, ok := m.Body.(*spectypes.PartialSignatureMessages); ok {
		isPostConsensusMessage = mm.Type == spectypes.PostConsensusPartialSig
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
		case isDecidedMesssage(state, m.SignedSSVMessage):
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
	case isDecidedMesssage(state, m.SignedSSVMessage):
		return 2
	case isConsensusMessage && specqbft.MessageType(m.SSVMessage.MsgType) == specqbft.CommitMsgType:
		return 1
	}
	return 0
}

// scoreConsensusType returns an integer score for the type of a consensus message.
// When given a non-consensus message, scoreConsensusType returns 0.
func scoreConsensusType(state *State, m *DecodedSSVMessage) int {
	if qbftMsg, ok := m.Body.(*specqbft.Message); ok {
		switch qbftMsg.MsgType {
		case specqbft.ProposalMsgType:
			return 4
		case specqbft.PrepareMsgType:
			return 3
		case specqbft.CommitMsgType:
			return 2
		case specqbft.RoundChangeMsgType:
			return 1
		}
	}
	return 0
}

func isDecidedMesssage(s *State, sm *spectypes.SignedSSVMessage) bool {
	if sm == nil {
		return false
	}
	return specqbft.MessageType(sm.SSVMessage.MsgType) == specqbft.CommitMsgType &&
		len(sm.OperatorIDs) > int(s.Quorum)
}
