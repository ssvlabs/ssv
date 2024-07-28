package types

import (
	"encoding/json"

	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
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
	Height qbft.Height
	Round  qbft.Round
}

type ExecuteDutyData struct {
	Duty *types.ValidatorDuty
}

type ExecuteCommitteeDutyData struct {
	Duty *types.CommitteeDuty
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

func Sign(msg *qbft.Message, operatorID types.OperatorID, operatorSigner OperatorSigner) (*types.SignedSSVMessage, error) {
	byts, err := msg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode message")
	}

	msgID := types.MessageID{}
	copy(msgID[:], msg.Identifier)

	ssvMsg := &types.SSVMessage{
		MsgType: types.SSVConsensusMsgType,
		MsgID:   msgID,
		Data:    byts,
	}

	sig, err := operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign SSVMessage")
	}

	signedSSVMessage := &types.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []types.OperatorID{operatorID},
		SSVMessage:  ssvMsg,
	}

	return signedSSVMessage, nil
}
