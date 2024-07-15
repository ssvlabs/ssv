package queue

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

var (
	ErrUnknownMessageType = fmt.Errorf("unknown message type")
	ErrDecodeNetworkMsg   = fmt.Errorf("could not decode data into an SSVMessage")
)

// DecodedSSVMessage is a bundle of SSVMessage and it's decoding.
// TODO: try to make it generic
type DecodedSSVMessage struct {
	SignedSSVMessage        *spectypes.SignedSSVMessage
	GenesisSignedSSVMessage *genesisspectypes.SignedSSVMessage
	*spectypes.SSVMessage

	// Body is the decoded Data.
	Body interface{} // *specqbft.Message | *spectypes.PartialSignatureMessages | *EventMsg | *genesisspecqbft.SignedMessage (TODO: remove post-fork) | *genesisspectypes.SignedPartialSignatureMessage (TODO: remove post-fork)
}

func (d *DecodedSSVMessage) Slot() (phase0.Slot, error) {
	switch m := d.Body.(type) {
	case *specqbft.Message: // TODO: Or message.SSVDecidedMsgType?
		return phase0.Slot(m.Height), nil
	case *spectypes.PartialSignatureMessages:
		return m.Slot, nil
	case *ssvtypes.EventMsg: // TODO: do we need slot in events?
		if m.Type == ssvtypes.Timeout {
			data, err := m.GetTimeoutData()
			if err != nil {
				return 0, ErrUnknownMessageType // TODO alan: other error
			}
			return phase0.Slot(data.Height), nil
		}
		return 0, ErrUnknownMessageType // TODO: alan: slot not supporting dutyexec msg?
	case *genesisspecqbft.SignedMessage: // TODO: remove post-fork
		return phase0.Slot(m.Message.Height), nil
	case *genesisspectypes.SignedPartialSignatureMessage: // TODO: remove post-fork
		return m.Message.Slot, nil
	default:
		return 0, ErrUnknownMessageType
	}
}

// DecodeSignedSSVMessage decodes a SignedSSVMessage into a DecodedSSVMessage.
func DecodeSignedSSVMessage(sm *spectypes.SignedSSVMessage) (*DecodedSSVMessage, error) {
	d, err := DecodeSSVMessage(sm.SSVMessage)
	if err != nil {
		return nil, err
	}
	d.SignedSSVMessage = sm
	return d, nil
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

// DecodeGenesisSSVMessage decodes a genesis SSVMessage into a DecodedSSVMessage.
func DecodeGenesisSSVMessage(m *genesisspectypes.SSVMessage) (*DecodedSSVMessage, error) {
	body, err := ExtractGenesisMsgBody(m)
	if err != nil {
		return nil, err
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

// DecodeGenesisSignedSSVMessage decodes a genesis SignedSSVMessage into a DecodedSSVMessage.
func DecodeGenesisSignedSSVMessage(sm *genesisspectypes.SignedSSVMessage) (*DecodedSSVMessage, error) {
	m, err := commons.DecodeGenesisNetworkMsg(sm.GetData())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecodeNetworkMsg, err)
	}

	d, err := DecodeGenesisSSVMessage(m)
	if err != nil {
		return nil, err
	}

	d.GenesisSignedSSVMessage = sm

	return d, nil
}

func ExtractGenesisMsgBody(m *genesisspectypes.SSVMessage) (any, error) {
	var body any
	switch m.MsgType {
	case genesisspectypes.SSVConsensusMsgType: // TODO: Or message.SSVDecidedMsgType?
		sm := &genesisspecqbft.SignedMessage{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, fmt.Errorf("failed to decode genesis SignedMessage: %w", err)
		}
		body = sm
	case genesisspectypes.SSVPartialSignatureMsgType:
		sm := &genesisspectypes.SignedPartialSignatureMessage{}
		if err := sm.Decode(m.Data); err != nil {
			return nil, fmt.Errorf("failed to decode genesis SignedPartialSignatureMessage: %w", err)
		}
		body = sm
	case genesisspectypes.MsgType(ssvmessage.SSVEventMsgType):
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
// The result will be 0 if equal, -1 if lower, 1 if higher.
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
// The result will be 0 if equal, -1 if lower, 1 if higher.
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
		case isDecidedMessage(state, m):
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
	case isDecidedMessage(state, m):
		return 2
	case isConsensusMessage && specqbft.MessageType(m.SSVMessage.MsgType) == specqbft.CommitMsgType:
		return 1
	}
	return 0
}

// scoreConsensusType returns an integer score for the type of consensus message.
// When given a non-consensus message, scoreConsensusType returns 0.
func scoreConsensusType(m *DecodedSSVMessage) int {
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

func isDecidedMessage(s *State, m *DecodedSSVMessage) bool {
	consensusMessage, isConsensusMessage := m.Body.(*specqbft.Message)
	if !isConsensusMessage {
		return false
	}
	return consensusMessage.MsgType == specqbft.CommitMsgType &&
		len(m.SignedSSVMessage.OperatorIDs) > int(s.Quorum)
}

// scoreCommitteeMessageSubtype returns an integer score for the message's type.
func scoreCommitteeMessageSubtype(state *State, m *DecodedSSVMessage, relativeHeight int) int {
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
			case isPostConsensusMessage:
				return 4
			case isConsensusMessage:
				return 3
			case isPreConsensusMessage:
				return 2
			}
			return 0
		}
		switch {
		case isPostConsensusMessage:
			return 3
		case isPreConsensusMessage:
			return 2
		case isConsensusMessage:
			return 1
		}
		return 0
	}

	// Higher height.
	if relativeHeight == 1 {
		switch {
		case isPostConsensusMessage:
			return 4
		case isDecidedMessage(state, m):
			return 3
		case isPreConsensusMessage:
			return 2
		case isConsensusMessage:
			return 1
		}
		return 0
	}

	// Lower height.
	switch {
	case isDecidedMessage(state, m):
		return 2
	case isConsensusMessage && specqbft.MessageType(m.SSVMessage.MsgType) == specqbft.CommitMsgType:
		return 1
	}
	return 0
}

// scoreCommitteeConsensusType returns an integer score for the type of committee consensus message.
// When given a non-consensus message, scoreConsensusType returns 0.
func scoreCommitteeConsensusType(m *DecodedSSVMessage) int {
	if qbftMsg, ok := m.Body.(*specqbft.Message); ok {
		switch qbftMsg.MsgType {
		case specqbft.CommitMsgType:
			return 4
		case specqbft.RoundChangeMsgType:
			return 3
		case specqbft.ProposalMsgType:
			return 2
		case specqbft.PrepareMsgType:
			return 1
		}
	}
	return 0
}
