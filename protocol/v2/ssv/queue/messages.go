package queue

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/wire"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

var (
	ErrUnknownMessageType = fmt.Errorf("unknown message type")
	ErrUnknownEventType   = fmt.Errorf("unknown event type")
)

// SSVMessage is a bundle of spectypes.SSVMessage and it's decoding.
type SSVMessage struct {
	SignedSSVMessage *spectypes.SignedSSVMessage
	*spectypes.SSVMessage

	// Body is the decoded Data.
	Body any // *specqbft.Message | *spectypes.PartialSignatureMessages | *EventMsg | *wire.Envelope (TBFT)
}

func (d *SSVMessage) DecodedSSVMessage() {}

func (d *SSVMessage) Slot() (phase0.Slot, error) {
	var errNilMessageBody = fmt.Errorf("nil SSVMessage body")

	if d.Body == nil {
		return 0, errNilMessageBody
	}

	switch m := d.Body.(type) {
	case *specqbft.Message:
		if m == nil {
			return 0, errNilMessageBody
		}
		return phase0.Slot(m.Height), nil
	case *spectypes.PartialSignatureMessages:
		if m == nil {
			return 0, errNilMessageBody
		}
		return m.Slot, nil
	case *ssvtypes.EventMsg:
		if m == nil {
			return 0, errNilMessageBody
		}
		switch m.Type {
		case ssvtypes.Timeout:
			data, err := m.GetTimeoutData()
			if err != nil {
				return 0, fmt.Errorf("get Timeout data: %w", err)
			}
			return data.Slot, nil
		case ssvtypes.ExecuteDuty:
			data, err := m.GetExecuteDutyData()
			if err != nil {
				return 0, fmt.Errorf("get ExecuteDuty data: %w", err)
			}
			if data.Duty == nil {
				return 0, fmt.Errorf("nil duty data in EventMsg")
			}
			return data.Duty.Slot, nil
		default:
			return 0, ErrUnknownEventType
		}
	default:
		return 0, ErrUnknownMessageType
	}
}

// DecodeSignedSSVMessage decodes a SignedSSVMessage into a SSVMessage.
func DecodeSignedSSVMessage(sm *spectypes.SignedSSVMessage) (*SSVMessage, error) {
	d, err := DecodeSSVMessage(sm.SSVMessage)
	if err != nil {
		return nil, err
	}
	d.SignedSSVMessage = sm
	return d, nil
}

// DecodeSSVMessage decodes a SSVMessage into a SSVMessage.
func DecodeSSVMessage(m *spectypes.SSVMessage) (*SSVMessage, error) {
	body, err := ExtractMsgBody(m)
	if err != nil {
		return nil, err
	}

	return &SSVMessage{
		SSVMessage: m,
		Body:       body,
	}, nil
}

func ExtractMsgBody(m *spectypes.SSVMessage) (any, error) {
	var body any
	switch m.MsgType {
	case spectypes.SSVConsensusMsgType:
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
	case ssvmessage.SSVTBFTMsgType:
		env, err := wire.Unwrap(m.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode TBFT envelope: %w", err)
		}
		body = env
	case ssvmessage.SSVDKGMsgType:
		// DKG envelopes are decoded by the per-cluster orchestrator (which
		// owns the kyber suite). The queue layer passes the raw bytes
		// through; the dispatcher reads m.SSVMessage.Data directly.
		body = nil
	default:
		return nil, ErrUnknownMessageType
	}

	return body, nil
}

// compareHeightOrSlot returns an integer comparing the message's height/slot to the current.
// The result will be 0 if equal, -1 if lower, 1 if higher.
//
// state.Slot doubles as the QBFT height: every runner starts its QBFT instance at height = slot,
// so we cast state.Slot to specqbft.Height when comparing QBFT messages.
func compareHeightOrSlot(state *State, m *SSVMessage) int {
	if qbftMsg, ok := m.Body.(*specqbft.Message); ok && qbftMsg != nil {
		stateHeight := specqbft.Height(state.Slot)
		if qbftMsg.Height == stateHeight {
			return 0
		}
		if qbftMsg.Height > stateHeight {
			return 1
		}
	} else if pms, ok := m.Body.(*spectypes.PartialSignatureMessages); ok && pms != nil { // everyone likes pms
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
// The result will be 0 if equal, -1 if lower, 1 if higher.
func scoreRound(state *State, m *SSVMessage) int {
	if qbftMsg, ok := m.Body.(*specqbft.Message); ok && qbftMsg != nil {
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
func scoreMessageType(m *SSVMessage) int {
	mm, ok := m.Body.(*ssvtypes.EventMsg)
	if !ok || mm == nil {
		return 0
	}

	switch mm.Type {
	case ssvtypes.ExecuteDuty:
		return 3
	case ssvtypes.Timeout:
		return 2
	default:
		return 0
	}
}

type messageClassification struct {
	isConsensusMessage     bool
	isPreConsensusMessage  bool
	isPostConsensusMessage bool
	consensusMsgType       specqbft.MessageType
}

func classifyMessage(m *SSVMessage) messageClassification {
	var classification messageClassification
	switch mm := m.Body.(type) {
	case *specqbft.Message:
		if mm != nil {
			classification.isConsensusMessage = true
			classification.consensusMsgType = mm.MsgType
		}
	case *spectypes.PartialSignatureMessages:
		if mm != nil {
			classification.isPostConsensusMessage = mm.Type == spectypes.PostConsensusPartialSig
			classification.isPreConsensusMessage = !classification.isPostConsensusMessage
		}
	}
	return classification
}

// scoreMessageSubtype returns an integer score for the message's type.
func scoreMessageSubtype(state *State, m *SSVMessage, relativeHeight int) int {
	classification := classifyMessage(m)

	// Current height.
	if relativeHeight == 0 {
		if state.HasRunningInstance {
			switch {
			case classification.isConsensusMessage:
				return 3
			case classification.isPreConsensusMessage:
				return 2
			case classification.isPostConsensusMessage:
				return 1
			}
			return 0
		}
		switch {
		case classification.isPreConsensusMessage:
			return 3
		case classification.isPostConsensusMessage:
			return 2
		case classification.isConsensusMessage:
			return 1
		}
		return 0
	}

	// Higher height.
	if relativeHeight == 1 {
		switch {
		case isDecidedMessage(state, m):
			return 4
		case classification.isPreConsensusMessage:
			return 3
		case classification.isConsensusMessage:
			return 2
		case classification.isPostConsensusMessage:
			return 1
		}
		return 0
	}

	// Lower height.
	switch {
	case isDecidedMessage(state, m):
		return 2
	case classification.isConsensusMessage && classification.consensusMsgType == specqbft.CommitMsgType:
		return 1
	}
	return 0
}

// scoreConsensusType returns an integer score for the type of consensus message.
// When given a non-consensus message, scoreConsensusType returns 0.
func scoreConsensusType(m *SSVMessage) int {
	if qbftMsg, ok := m.Body.(*specqbft.Message); ok && qbftMsg != nil {
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

func isDecidedMessage(s *State, m *SSVMessage) bool {
	consensusMessage, isConsensusMessage := m.Body.(*specqbft.Message)
	if !isConsensusMessage || consensusMessage == nil {
		return false
	}
	return consensusMessage.MsgType == specqbft.CommitMsgType &&
		uint64(len(m.SignedSSVMessage.OperatorIDs)) > s.Quorum
}

// scoreCommitteeMessageSubtype returns an integer score for the message's type.
func scoreCommitteeMessageSubtype(state *State, m *SSVMessage, relativeHeight int) int {
	classification := classifyMessage(m)

	// Current height.
	if relativeHeight == 0 {
		if state.HasRunningInstance {
			switch {
			case classification.isPostConsensusMessage:
				return 4
			case classification.isConsensusMessage:
				return 3
			case classification.isPreConsensusMessage:
				return 2
			}
			return 0
		}
		switch {
		case classification.isPostConsensusMessage:
			return 3
		case classification.isPreConsensusMessage:
			return 2
		case classification.isConsensusMessage:
			return 1
		}
		return 0
	}

	// Higher height.
	if relativeHeight == 1 {
		switch {
		case classification.isPostConsensusMessage:
			return 4
		case isDecidedMessage(state, m):
			return 3
		case classification.isPreConsensusMessage:
			return 2
		case classification.isConsensusMessage:
			return 1
		}
		return 0
	}

	// Lower height.
	switch {
	case isDecidedMessage(state, m):
		return 2
	case classification.isConsensusMessage && classification.consensusMsgType == specqbft.CommitMsgType:
		return 1
	}
	return 0
}
