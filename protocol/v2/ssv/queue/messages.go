package queue

import (
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

// DecodedSSVMessage is a bundle of SSVMessage and it's decoding.
type DecodedSSVMessage struct {
	*types.SSVMessage

	// Body is the decoded Data.
	Body interface{} // *SignedMessage | *SignedPartialSignatureMessage
}

// DecodeSSVMessage decodes an SSVMessage and returns a DecodedSSVMessage.
func DecodeSSVMessage(logger *zap.Logger, m *spectypes.SSVMessage) (*DecodedSSVMessage, error) {
	var body interface{}
	switch m.MsgType {
	case types.SSVConsensusMsgType: // TODO: Or message.SSVDecidedMsgType?
		sm := &qbft.SignedMessage{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, errors.Wrap(err, "failed to decode SignedMessage")
		}
		body = sm
	case types.SSVPartialSignatureMsgType:
		sm := &spectypes.SignedPartialSignatureMessage{}
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

// scoreRound returns an integer comparing the message's round (if exist) to the current.
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func scoreRound(state *State, m *DecodedSSVMessage) int {
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
	consensusMessage, isConsensusMessage := m.Body.(*qbft.SignedMessage)

	var (
		isPreConsensusMessage  = false
		isPostConsensusMessage = false
	)
	if mm, ok := m.Body.(*spectypes.SignedPartialSignatureMessage); ok {
		isPostConsensusMessage = mm.Message.Type == spectypes.PostConsensusPartialSig
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
	case isConsensusMessage && consensusMessage.Message.MsgType == qbft.CommitMsgType:
		return 1
	}
	return 0
}

// scoreConsensusType returns an integer score for the type of a consensus message.
// When given a non-consensus message, scoreConsensusType returns 0.
func scoreConsensusType(state *State, m *DecodedSSVMessage) int {
	if mm, ok := m.Body.(*qbft.SignedMessage); ok {
		switch mm.Message.MsgType {
		case qbft.ProposalMsgType:
			return 4
		case qbft.PrepareMsgType:
			return 3
		case qbft.CommitMsgType:
			return 2
		case qbft.RoundChangeMsgType:
			return 1
		}
	}
	return 0
}

func isDecidedMesssage(s *State, sm *qbft.SignedMessage) bool {
	if sm == nil {
		return false
	}
	return sm.Message.MsgType == qbft.CommitMsgType &&
		len(sm.Signers) > int(s.Quorum)
}
