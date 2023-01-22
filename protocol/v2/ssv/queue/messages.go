package queue

import (
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
)

// DecodedSSVMessage is a bundle of SSVMessage and it's decoding.
type DecodedSSVMessage struct {
	*types.SSVMessage

	// Body is the decoded Data.
	Body interface{} // *SignedMessage | *SignedPartialSignatureMessage
}

// DecodeSSVMessage decodes an SSVMessage and returns a DecodedSSVMessage.
func DecodeSSVMessage(m *types.SSVMessage) (*DecodedSSVMessage, error) {
	var body interface{}
	switch m.MsgType {
	case types.SSVConsensusMsgType: // TODO: Or message.SSVDecidedMsgType?
		sm := &qbft.SignedMessage{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode SignedMessage")
		}
		body = sm
	case types.SSVPartialSignatureMsgType:
		sm := &ssv.SignedPartialSignatureMessage{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode SignedPartialSignatureMessage")
		}
		body = sm
	case ssvmessage.SSVEventMsgType:
		msg := &ssvtypes.EventMsg{}
		if err := msg.Decode(m.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode EventMsg")
		}
		body = msg
	}
	return &DecodedSSVMessage{
		SSVMessage: m,
		Body:       body,
	}, nil
}

// compareHeightOrSlot returns an integer comparing the message's height/slot to the current.
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func compareHeightOrSlot(state *State, m *DecodedSSVMessage) int {
	if mm, ok := m.Body.(*qbft.SignedMessage); ok {
		if mm.Message.Height == state.Height {
			return 0
		}
		if mm.Message.Height > state.Height {
			return 1
		}
		// TODO
		// } else if mm, ok := m.Body.(*ssv.SignedPartialSignatureMessage); ok {
		// 	if mm.Message.Messages[0].Slot == state.Slot {
		// 		return 0
		// 	}
		// 	if mm.Message.Messages[0].Slot > state.Slot {
		// 		return 1
		// 	}
	}
	return -1
}

// compareRound returns an integer comparing the message's round (if exist) to the current.
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func compareRound(state *State, m *DecodedSSVMessage) int {
	if mm, ok := m.Body.(*qbft.SignedMessage); ok {
		if mm.Message.Round == state.Round {
			return 2
		}
		if mm.Message.Round > state.Round {
			return 1
		}
		return -1
	}
	return 0
}

// messageScore returns a score based on the top level message type,
// where event type messages are prioritized over other types.
func messageScore(m *DecodedSSVMessage) int {
	switch m.MsgType {
	case ssvmessage.SSVEventMsgType:
		return 1 + scoreByPrecedence(nil, m, isMessageEventOfType(ssvtypes.ExecuteDuty), isMessageEventOfType(ssvtypes.Timeout))
	default:
		return 0
	}
}

// messageTypeScore returns an integer score for the message's type.
func messageTypeScore(state *State, m *DecodedSSVMessage, relativeHeight int) int {
	// Current.
	if relativeHeight == 0 {
		if state.HasRunningInstance {
			return scoreByPrecedence(state, m,
				isConsensusMessage, isPreConsensusMessage, isPostConsensusMessage)
		}
		return scoreByPrecedence(state, m,
			isPreConsensusMessage, isPostConsensusMessage, isConsensusMessage)
	}

	// Higher.
	if relativeHeight == 1 {
		return scoreByPrecedence(state, m,
			isDecidedMesssage, isPreConsensusMessage, isConsensusMessage, isPostConsensusMessage)
	}

	// Lower.
	return scoreByPrecedence(state, m,
		isDecidedMesssage, isMessageOfType(qbft.CommitMsgType),
	)
}

// consensusTypeScore returns an integer score for the type of a consensus message.
// When given a non-consensus message, consensusTypeScore returns 0.
func consensusTypeScore(state *State, m *DecodedSSVMessage) int {
	if isConsensusMessage(state, m) {
		return scoreByPrecedence(state, m,
			isMessageOfType(qbft.ProposalMsgType), isMessageOfType(qbft.PrepareMsgType),
			isMessageOfType(qbft.CommitMsgType), isMessageOfType(qbft.RoundChangeMsgType))
	}
	return 0
}

// messageCondition returns whether the given message complies to a condition within the given state.
type messageCondition func(s *State, m *DecodedSSVMessage) bool

// scoreByPrecedence returns the inverted index of the first true within the given conditions,
// so that the earlier the true, the higher the score.
func scoreByPrecedence(s *State, m *DecodedSSVMessage, conditions ...messageCondition) int {
	for i, check := range conditions {
		if check(s, m) {
			return len(conditions) - i
		}
	}
	return 0
}

func isConsensusMessage(s *State, m *DecodedSSVMessage) bool {
	return m.MsgType == types.SSVConsensusMsgType
}

func isPreConsensusMessage(s *State, m *DecodedSSVMessage) bool {
	if m.MsgType != types.SSVPartialSignatureMsgType {
		return false
	}
	if sm, ok := m.Body.(*ssv.SignedPartialSignatureMessage); ok {
		return sm.Message.Type != ssv.PostConsensusPartialSig
	}
	return false
}

func isPostConsensusMessage(s *State, m *DecodedSSVMessage) bool {
	if m.MsgType != types.SSVPartialSignatureMsgType {
		return false
	}
	if sm, ok := m.Body.(*ssv.SignedPartialSignatureMessage); ok {
		return sm.Message.Type == ssv.PostConsensusPartialSig
	}
	return false
}

func isDecidedMesssage(s *State, m *DecodedSSVMessage) bool {
	if sm, ok := m.Body.(*qbft.SignedMessage); ok {
		return sm.Message.MsgType == qbft.CommitMsgType &&
			len(sm.Signers) > int(s.Quorum)
	}
	return false
}

func isMessageOfType(t qbft.MessageType) messageCondition {
	return func(s *State, m *DecodedSSVMessage) bool {
		if sm, ok := m.Body.(*qbft.SignedMessage); ok {
			return sm.Message.MsgType == t
		}
		return false
	}
}

func isMessageEventOfType(eventType ssvtypes.EventType) messageCondition {
	return func(s *State, m *DecodedSSVMessage) bool {
		if em, ok := m.Body.(*ssvtypes.EventMsg); ok {
			return em.Type == eventType
		}
		return false
	}
}
