package genesisqueue

import (
	"fmt"

	preforkphase0 "github.com/AKorpusenko/genesis-go-eth2-client/spec/phase0"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/ssvlabs/ssv/network/commons"
	ssvmessage "github.com/ssvlabs/ssv/protocol/genesis/message"
	genesisssvtypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	ssvtypes "github.com/ssvlabs/ssv/protocol/genesis/types"
)

var (
	ErrUnknownMessageType = fmt.Errorf("unknown message type")
	ErrDecodeNetworkMsg   = fmt.Errorf("could not decode data into an SSVMessage")
)

type GenesisSSVMessage struct {
	SignedSSVMessage *genesisspectypes.SignedSSVMessage
	*genesisspectypes.SSVMessage

	// Body is the decoded Data.
	Body interface{} // *EventMsg | *genesisspecqbft.SignedMessage | *genesisspectypes.SignedPartialSignatureMessage
}

func (d *GenesisSSVMessage) Slot() (phase0.Slot, error) {
	switch m := d.Body.(type) {
	case *genesisssvtypes.EventMsg: // TODO: do we need slot in events?
		if m.Type == genesisssvtypes.Timeout {
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
		return phase0.Slot(m.Message.Slot), nil
	default:
		return 0, ErrUnknownMessageType
	}
}

// DecodeGenesisSSVMessage decodes a genesis SSVMessage into a GenesisSSVMessage.
func DecodeGenesisSSVMessage(m *genesisspectypes.SSVMessage) (*GenesisSSVMessage, error) {

	body, err := ExtractGenesisMsgBody(m)
	if err != nil {
		return nil, err
	}

	return &GenesisSSVMessage{
		SSVMessage: &genesisspectypes.SSVMessage{
			MsgType: m.MsgType,
			MsgID:   m.MsgID,
			Data:    m.Data,
		},
		Body: body,
	}, nil
}

// DecodeGenesisSignedSSVMessage decodes a genesis SignedSSVMessage into a GenesisSSVMessage.
func DecodeGenesisSignedSSVMessage(sm *genesisspectypes.SignedSSVMessage) (*GenesisSSVMessage, error) {
	m, err := commons.DecodeGenesisNetworkMsg(sm.GetData())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecodeNetworkMsg, err)
	}

	d, err := DecodeGenesisSSVMessage(m)
	if err != nil {
		return nil, err
	}

	d.SignedSSVMessage = sm

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
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func compareHeightOrSlot(state *State, m *GenesisSSVMessage) int {
	if mm, ok := m.Body.(*genesisspecqbft.SignedMessage); ok {
		if mm.Message.Height == state.Height {
			return 0
		}
		if mm.Message.Height > state.Height {
			return 1
		}
	} else if mm, ok := m.Body.(*genesisspectypes.SignedPartialSignatureMessage); ok {
		if mm.Message.Slot == preforkphase0.Slot(state.Slot) {
			return 0
		}
		if mm.Message.Slot > preforkphase0.Slot(state.Slot) {
			return 1
		}
	}
	return -1
}

// scoreRound returns an integer comparing the message's round (if exist) to the current.
// The reuslt will be 0 if equal, -1 if lower, 1 if higher.
func scoreRound(state *State, m *GenesisSSVMessage) int {
	if mm, ok := m.Body.(*genesisspecqbft.SignedMessage); ok {
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
func scoreMessageType(m *GenesisSSVMessage) int {
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
func scoreMessageSubtype(state *State, m *GenesisSSVMessage, relativeHeight int) int {
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
func scoreConsensusType(state *State, m *GenesisSSVMessage) int {
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
