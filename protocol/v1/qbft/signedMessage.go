package qbft

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/bloxapp/ssv/protocol/v1"
	"github.com/bloxapp/ssv/protocol/v1/crypto"
	"github.com/bloxapp/ssv/protocol/v1/operator/types"
	"github.com/pkg/errors"
)

type MessageType int

const (
	ProposalMsgType MessageType = iota
	PrepareMsgType
	CommitMsgType
	RoundChangeMsgType
	DecidedMsgType
)

type ProposalData struct {
	Data                     []byte
	RoundChangeJustification []*SignedMessage
	PrepareJustification     []*SignedMessage
}

// Encode returns a msg encoded bytes or error
func (d *ProposalData) Encode() ([]byte, error) {
	return json.Marshal(d)
}

// Decode returns error if decoding failed
func (d *ProposalData) Decode(data []byte) error {
	return json.Unmarshal(data, &d)
}

type PrepareData struct {
	Data []byte
}

// Encode returns a msg encoded bytes or error
func (d *PrepareData) Encode() ([]byte, error) {
	return json.Marshal(d)
}

// Decode returns error if decoding failed
func (d *PrepareData) Decode(data []byte) error {
	return json.Unmarshal(data, &d)
}

type CommitData struct {
	Data []byte
}

// Encode returns a msg encoded bytes or error
func (d *CommitData) Encode() ([]byte, error) {
	return json.Marshal(d)
}

// Decode returns error if decoding failed
func (d *CommitData) Decode(data []byte) error {
	return json.Unmarshal(data, &d)
}

type RoundChangeData interface {
	GetPreparedValue() []byte
	GetPreparedRound() Round
	// GetNextProposalData returns NOT nil byte array if the signer is the next round's proposal.
	GetNextProposalData() []byte
	// GetRoundChangeJustification returns signed prepare messages for the last prepared state
	GetRoundChangeJustification() []*SignedMessage
}

type Message struct {
	MsgType    MessageType
	Height     Height // QBFT instance Height
	Round      Round  // QBFT round for which the msg is for
	Identifier []byte // instance Identifier this msg belongs to
	Data       []byte
}

// GetProposalData returns proposal specific data
func (msg *Message) GetProposalData() (*ProposalData, error) {
	ret := &ProposalData{}
	if err := ret.Decode(msg.Data); err != nil {
		return nil, errors.Wrap(err, "could not decode proposal data from message")
	}
	return ret, nil
}

// GetPrepareData returns prepare specific data
func (msg *Message) GetPrepareData() (*PrepareData, error) {
	ret := &PrepareData{}
	if err := ret.Decode(msg.Data); err != nil {
		return nil, errors.Wrap(err, "could not decode prepare data from message")
	}
	return ret, nil
}

// GetCommitData returns commit specific data
func (msg *Message) GetCommitData() (*CommitData, error) {
	ret := &CommitData{}
	if err := ret.Decode(msg.Data); err != nil {
		return nil, errors.Wrap(err, "could not decode commit data from message")
	}
	return ret, nil
}

// GetRoundChangeData returns round change specific data
func (msg *Message) GetRoundChangeData() RoundChangeData {
	panic("implement")
}

// Encode returns a msg encoded bytes or error
func (msg *Message) Encode() ([]byte, error) {
	return json.Marshal(msg)
}

// Decode returns error if decoding failed
func (msg *Message) Decode(data []byte) error {
	return json.Unmarshal(data, &msg)
}

// GetRoot returns the root used for signing and verification
func (msg *Message) GetRoot() ([]byte, error) {
	marshaledRoot, err := msg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode message")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

// DeepCopy returns a new instance of Message, deep copied
func (msg *Message) DeepCopy() *Message {
	panic("implement")
}

type SignedMessage struct {
	Signature crypto.Signature
	Signers   []types.OperatorID
	Message   *Message // message for which this signature is for
}

func (signedMsg *SignedMessage) GetSignature() crypto.Signature {
	return signedMsg.Signature
}
func (signedMsg *SignedMessage) GetSigners() []types.OperatorID {
	return signedMsg.Signers
}

// MatchedSigners returns true if the provided signer ids are equal to GetSignerIds() without order significance
func (signedMsg *SignedMessage) MatchedSigners(ids []types.OperatorID) bool {
	for _, id := range signedMsg.Signers {
		found := false
		for _, id2 := range ids {
			if id == id2 {
				found = true
			}
		}

		if !found {
			return false
		}
	}
	return true
}

// MutualSigners returns true if signatures have at least 1 mutual signer
func (signedMsg *SignedMessage) MutualSigners(sig v1.MessageSignature) bool {
	for _, id := range signedMsg.Signers {
		for _, id2 := range sig.GetSigners() {
			if id == id2 {
				return true
			}
		}
	}
	return false
}

// Aggregate will aggregate the signed message if possible (unique signers, same digest, valid)
func (signedMsg *SignedMessage) Aggregate(sig v1.MessageSignature) error {
	if signedMsg.MutualSigners(sig) {
		return errors.New("can't aggregate 2 signed messages with mutual signers")
	}

	aggregated, err := signedMsg.Signature.Aggregate(sig.GetSignature())
	if err != nil {
		return errors.Wrap(err, "could not aggregate signatures")
	}
	signedMsg.Signature = aggregated
	signedMsg.Signers = append(signedMsg.Signers, sig.GetSigners()...)

	return nil
}

// Encode returns a msg encoded bytes or error
func (signedMsg *SignedMessage) Encode() ([]byte, error) {
	return json.Marshal(signedMsg)
}

// Decode returns error if decoding failed
func (signedMsg *SignedMessage) Decode(data []byte) error {
	return json.Unmarshal(data, &signedMsg)
}

// GetRoot returns the root used for signing and verification
func (signedMsg *SignedMessage) GetRoot() ([]byte, error) {
	return signedMsg.Message.GetRoot()
}

// DeepCopy returns a new instance of SignedMessage, deep copied
func (signedMsg *SignedMessage) DeepCopy() *SignedMessage {
	ret := &SignedMessage{
		Signers:   make([]types.OperatorID, len(signedMsg.Signers)),
		Signature: make([]byte, len(signedMsg.Signature)),
	}
	copy(ret.Signers, signedMsg.Signers)
	copy(ret.Signature, signedMsg.Signature)

	ret.Message = &Message{
		MsgType:    signedMsg.Message.MsgType,
		Height:     signedMsg.Message.Height,
		Round:      signedMsg.Message.Round,
		Identifier: make([]byte, len(signedMsg.Message.Identifier)),
		Data:       make([]byte, len(signedMsg.Message.Data)),
	}
	copy(ret.Message.Identifier, signedMsg.Message.Identifier)
	copy(ret.Message.Data, signedMsg.Message.Data)
	return ret
}
