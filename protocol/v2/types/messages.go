package types

import (
	"encoding/json"
	"fmt"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type EventType int

const (
	// Timeout in order to run timeoutData process
	Timeout EventType = iota
	// ExecuteDuty for when to start duty runner
	ExecuteDuty
)

func (e EventType) String() string {
	switch e {
	case Timeout:
		return "timeoutData"
	case ExecuteDuty:
		return "executeDuty"
	default:
		return "unknown"
	}
}

type EventMsg struct {
	Type EventType
	Data []byte
}

type TimeoutData struct {
	Height specqbft.Height
	Round  specqbft.Round
}

type ExecuteDutyData struct {
	Duty *spectypes.BeaconDuty
}

type ExecuteCommitteeDutyData struct {
	Duty *spectypes.CommitteeDuty
}

func (m *EventMsg) GetTimeoutData() (*TimeoutData, error) {
	td := &TimeoutData{}
	if err := json.Unmarshal(m.Data, td); err != nil {
		return nil, err
	}
	return td, nil
}

func (m *EventMsg) GetExecuteDutyData() (*ExecuteDutyData, error) {
	ed := &ExecuteDutyData{}
	if err := json.Unmarshal(m.Data, ed); err != nil {
		return nil, err
	}
	return ed, nil
}

func (m *EventMsg) GetExecuteCommitteeDutyData() (*ExecuteCommitteeDutyData, error) {
	ed := &ExecuteCommitteeDutyData{}
	if err := json.Unmarshal(m.Data, ed); err != nil {
		return nil, err
	}
	return ed, nil
}

// Encode returns a msg encoded bytes or error
func (m *EventMsg) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// Decode returns error if decoding failed
func (m *EventMsg) Decode(data []byte) error {
	return json.Unmarshal(data, &m)
}

func SignedSSVMessageFromGenesis(genesisMsg *genesisspectypes.SignedSSVMessage) (*spectypes.SignedSSVMessage, error) {
	genesisSSVMsg, err := genesisMsg.GetSSVMessageFromData()
	if err != nil {
		return nil, err
	}

	ssvMsg, fullData, err := SSVMessageFromGenesis(genesisSSVMsg)
	if err != nil {
		return nil, err
	}

	encodedSSVMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{genesisMsg.Signature},
		OperatorIDs: []spectypes.OperatorID{genesisMsg.OperatorID},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.MsgType(genesisSSVMsg.MsgType),
			MsgID:   spectypes.MessageID(genesisSSVMsg.MsgID),
			Data:    encodedSSVMsg,
		},
		FullData: fullData,
	}, nil
}

func SSVMessageFromGenesis(genesisMsg *genesisspectypes.SSVMessage) (*spectypes.SSVMessage, []byte, error) {
	var data []byte
	var fullData []byte

	switch genesisMsg.MsgType {
	case genesisspectypes.SSVConsensusMsgType: // TODO: Or message.SSVDecidedMsgType?
		genesisQBFTMsg := &genesisspecqbft.SignedMessage{}
		if err := genesisQBFTMsg.Decode(genesisMsg.Data); err != nil {
			return nil, nil, fmt.Errorf("failed to decode genesis SignedMessage: %w", err)
		}
		qbftMsg := specqbft.Message{
			MsgType:                  specqbft.MessageType(genesisQBFTMsg.Message.MsgType),
			Height:                   specqbft.Height(genesisQBFTMsg.Message.Height),
			Round:                    specqbft.Round(genesisQBFTMsg.Message.Round),
			Identifier:               genesisQBFTMsg.Message.Identifier,
			Root:                     genesisQBFTMsg.Message.Root,
			DataRound:                specqbft.Round(genesisQBFTMsg.Message.DataRound),
			RoundChangeJustification: genesisQBFTMsg.Message.RoundChangeJustification,
			PrepareJustification:     genesisQBFTMsg.Message.PrepareJustification,
		}
		encodedMsg, err := qbftMsg.Encode()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode QBFT message: %w", err)
		}

		data = encodedMsg
		fullData = genesisQBFTMsg.FullData

	case genesisspectypes.SSVPartialSignatureMsgType:
		genesisSPSM := &genesisspectypes.SignedPartialSignatureMessage{}
		if err := genesisSPSM.Decode(genesisMsg.Data); err != nil {
			return nil, nil, fmt.Errorf("failed to decode genesis SignedPartialSignatureMessage: %w", err)
		}

		psm := spectypes.PartialSignatureMessages{
			Type: spectypes.PartialSigMsgType(genesisSPSM.Message.Type),
			Slot: genesisSPSM.Message.Slot,
		}

		for _, msg := range genesisSPSM.Message.Messages {
			psm.Messages = append(psm.Messages, &spectypes.PartialSignatureMessage{
				PartialSignature: spectypes.Signature(msg.PartialSignature),
				SigningRoot:      msg.SigningRoot,
				Signer:           msg.Signer,
				ValidatorIndex:   0, // TODO
			})
		}

		encodedMsg, err := psm.Encode()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode QBFT message: %w", err)
		}

		data = encodedMsg

	default:
		return nil, nil, fmt.Errorf("unknown message type %d", genesisMsg.MsgType)
	}

	msg := &spectypes.SSVMessage{
		MsgType: spectypes.MsgType(genesisMsg.MsgType),
		MsgID:   spectypes.MessageID(genesisMsg.MsgID),
		Data:    data,
	}
	return msg, fullData, nil
}
