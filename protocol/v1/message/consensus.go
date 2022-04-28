package message

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// ErrDuplicateMsgSigner is thrown when trying to sign multiple times with the same signer
var ErrDuplicateMsgSigner = errors.New("can't aggregate 2 signed messages with mutual signers")

// Signer is an interface responsible for consensus messages signing
/*type Signer interface { TODO already have this interface in beaconprotocol.client
	// SignIBFTMessage signs a network iBFT msg
	SignIBFTMessage(message *ConsensusMessage, pk []byte) ([]byte, error)
}*/

// ConsensusMessageType is the type of consensus messages
type ConsensusMessageType int

const (
	// ProposalMsgType is the type used for proposal messages
	ProposalMsgType ConsensusMessageType = iota
	// PrepareMsgType is the type used for prepare messages
	PrepareMsgType
	// CommitMsgType is the type used for commit messages
	CommitMsgType
	// RoundChangeMsgType is the type used for change round messages
	RoundChangeMsgType
	//// DecidedMsgType is the type used for decided messages
	//DecidedMsgType
)

// String is the string representation of ConsensusMessageType
func (cmt ConsensusMessageType) String() string {
	switch cmt {
	case ProposalMsgType:
		return "propose"
	case PrepareMsgType:
		return "prepare"
	case CommitMsgType:
		return "commit"
	case RoundChangeMsgType:
		return "change_round"
	//case DecidedMsgType:
	//	return "decided"
	default:
		return "unknown"
	}
}

// ProposalData is the structure used for propose messages
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
	return json.Unmarshal(data, d)
}

// PrepareData is the structure used for prepare messages
type PrepareData struct {
	Data []byte
}

// Encode returns a msg encoded bytes or error
func (d *PrepareData) Encode() ([]byte, error) {
	return json.Marshal(d)
}

// Decode returns error if decoding failed
func (d *PrepareData) Decode(data []byte) error {
	return json.Unmarshal(data, d)
}

// CommitData is the structure used for commit messages
type CommitData struct {
	Data []byte
}

// Encode returns a msg encoded bytes or error
func (d *CommitData) Encode() ([]byte, error) {
	return json.Marshal(d)
}

// Decode returns error if decoding failed
func (d *CommitData) Decode(data []byte) error {
	return json.Unmarshal(data, d)
}

// Round is the QBFT round of the message
type Round uint64

//func (r Round) toUint64() uint64 {
//	return uint64(r)
//}

// Height is the height of the QBFT instance
type Height int64

//func (r Height) toInt64() int64 {
//	return int64(r)
//}

// RoundChangeData represents the data that is sent upon change round
type RoundChangeData struct {
	PreparedValue            []byte
	Round                    Round
	NextProposalData         []byte
	RoundChangeJustification []*SignedMessage
}

// GetPreparedValue return prepared value
func (r *RoundChangeData) GetPreparedValue() []byte {
	return r.PreparedValue
}

// GetPreparedRound return prepared round
func (r *RoundChangeData) GetPreparedRound() Round {
	return r.Round
}

// GetNextProposalData returns NOT nil byte array if the signer is the next round's proposal.
func (r *RoundChangeData) GetNextProposalData() []byte {
	return r.NextProposalData
}

// GetRoundChangeJustification returns signed prepare messages for the last prepared state
func (r *RoundChangeData) GetRoundChangeJustification() []*SignedMessage {
	return r.RoundChangeJustification
}

// Encode returns a msg encoded bytes or error
func (r *RoundChangeData) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *RoundChangeData) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

// ConsensusMessage is the structure used for consensus messages
type ConsensusMessage struct {
	MsgType    ConsensusMessageType
	Height     Height     // QBFT instance Height
	Round      Round      // QBFT round for which the msg is for
	Identifier Identifier // instance Identifier this msg belongs to
	Data       []byte
}

// GetProposalData returns proposal specific data
func (msg *ConsensusMessage) GetProposalData() (*ProposalData, error) {
	ret := &ProposalData{}
	if err := ret.Decode(msg.Data); err != nil {
		return nil, errors.Wrap(err, "could not decode proposal data from message")
	}
	return ret, nil
}

// GetPrepareData returns prepare specific data
func (msg *ConsensusMessage) GetPrepareData() (*PrepareData, error) {
	ret := &PrepareData{}
	if err := ret.Decode(msg.Data); err != nil {
		return nil, errors.Wrap(err, "could not decode prepare data from message")
	}
	return ret, nil
}

// GetCommitData returns commit specific data
func (msg *ConsensusMessage) GetCommitData() (*CommitData, error) {
	ret := &CommitData{}
	if err := ret.Decode(msg.Data); err != nil {
		return nil, errors.Wrap(err, "could not decode commit data from message")
	}
	return ret, nil
}

// GetRoundChangeData returns round change specific data
func (msg *ConsensusMessage) GetRoundChangeData() (*RoundChangeData, error) {
	ret := &RoundChangeData{}
	if err := ret.Decode(msg.Data); err != nil {
		return nil, errors.Wrap(err, "could not decode change round data from message")
	}
	return ret, nil
}

// Encode returns a msg encoded bytes or error
func (msg *ConsensusMessage) Encode() ([]byte, error) {
	return json.Marshal(msg)
}

// Decode returns error if decoding failed
func (msg *ConsensusMessage) Decode(data []byte) error {
	return json.Unmarshal(data, msg)
}

// GetRoot returns the root used for signing and verification
func (msg *ConsensusMessage) GetRoot(forkVersion string) ([]byte, error) {
	// using string version for checking in order to prevent cycle dependency

	if forkVersion == "v0" {
		return msg.convertToV0Root()
	}

	// use v1 encoded struct
	marshaledRoot, err := msg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode message")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

// DeepCopy returns a new instance of ConsensusMessage, deep copied
func (msg *ConsensusMessage) DeepCopy() *ConsensusMessage {
	panic("implement")
}

// Sign takes a secret key and signs the Message
func (msg *ConsensusMessage) Sign(sk *bls.SecretKey, forkVersion string) (*bls.Sign, error) {
	root, err := msg.GetRoot(forkVersion)
	if err != nil {
		return nil, err
	}
	return sk.SignByte(root), nil
}

// SignedMessage contains a message and the corresponding signature + signers list
type SignedMessage struct {
	Signature Signature
	Signers   []OperatorID
	Message   *ConsensusMessage // message for which this signature is for
}

// GetSignature returns the message signature
func (signedMsg *SignedMessage) GetSignature() Signature {
	return signedMsg.Signature
}

// GetSigners returns the message signers
func (signedMsg *SignedMessage) GetSigners() []OperatorID {
	return signedMsg.Signers
}

// MatchedSigners returns true if the provided signer ids are equal to GetSignerIds() without order significance
func (signedMsg *SignedMessage) MatchedSigners(ids []OperatorID) bool {
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
func (signedMsg *SignedMessage) MutualSigners(sig MsgSignature) bool {
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
func (signedMsg *SignedMessage) Aggregate(sigs ...MsgSignature) error {
	for _, sig := range sigs {
		if signedMsg.MutualSigners(sig) {
			return ErrDuplicateMsgSigner
		}

		aggregated, err := signedMsg.Signature.Aggregate(sig.GetSignature())
		if err != nil {
			return errors.Wrap(err, "could not aggregate signatures")
		}
		signedMsg.Signature = aggregated
		signedMsg.Signers = append(signedMsg.Signers, sig.GetSigners()...)
	}
	return nil
}

// Encode returns a msg encoded bytes or error
func (signedMsg *SignedMessage) Encode() ([]byte, error) {
	return json.Marshal(signedMsg)
}

// Decode returns error if decoding failed
func (signedMsg *SignedMessage) Decode(data []byte) error {
	return json.Unmarshal(data, signedMsg)
}

// GetRoot returns the root used for signing and verification
func (signedMsg *SignedMessage) GetRoot(forkVersion string) ([]byte, error) {
	return signedMsg.Message.GetRoot(forkVersion)
}

// DeepCopy returns a new instance of SignedMessage, deep copied
func (signedMsg *SignedMessage) DeepCopy() *SignedMessage {
	ret := &SignedMessage{
		Signers:   make([]OperatorID, len(signedMsg.Signers)),
		Signature: make([]byte, len(signedMsg.Signature)),
	}
	copy(ret.Signers, signedMsg.Signers)
	copy(ret.Signature, signedMsg.Signature)

	ret.Message = &ConsensusMessage{
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

func (msg *ConsensusMessage) convertToV0Root() ([]byte, error) {
	var m OrderedMap // must use ordered map!

	var data []byte
	switch msg.MsgType {
	case ProposalMsgType:
		m = append(m, KeyVal{"type", proto.RoundState_PrePrepare})
		if p, err := msg.GetProposalData(); err != nil {
			return nil, err
		} else {
			data = p.Data
		}
	case PrepareMsgType:
		m = append(m, KeyVal{"type", proto.RoundState_Prepare})
		if p, err := msg.GetPrepareData(); err != nil {
			return nil, err
		} else {
			data = p.Data
		}
	case CommitMsgType:
		m = append(m, KeyVal{"type", proto.RoundState_Commit})
		if c, err := msg.GetCommitData(); err != nil {
			return nil, err
		} else {
			data = c.Data
		}
	case RoundChangeMsgType:
		m = append(m, KeyVal{"type", proto.RoundState_ChangeRound})
		if cr, err := msg.GetRoundChangeData(); err != nil {
			return nil, err
		} else {
			data = cr.PreparedValue
		}
	default:
		return nil, errors.Errorf("consensus type is not known. type - %s", msg.MsgType.String())
	}

	m = append(m, KeyVal{"round", int64(msg.Round)})
	m = append(m, KeyVal{"lambda", []byte(format.IdentifierFormat(msg.Identifier.GetValidatorPK(), msg.Identifier.GetRoleType().String()))})
	m = append(m, KeyVal{"seq_number", int64(msg.Height)}) // TODO seq 0 is not showing on old struct?
	m = append(m, KeyVal{"value", data})

	marshaledRoot, err := m.MarshalJSON()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode message")
	}

	hasher := sha256.New()
	_, err = hasher.Write(marshaledRoot)
	if err != nil {
		return nil, err
	}
	return hasher.Sum(nil), nil
}

type KeyVal struct {
	Key string
	Val interface{}
}

// OrderedMap Define an ordered map
type OrderedMap []KeyVal

// MarshalJSON Implement the json.Marshaler interface
func (omap OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("{")
	for i, kv := range omap {
		if i != 0 {
			buf.WriteString(",")
		}
		// marshal key
		key, err := json.Marshal(kv.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteString(":")
		// marshal value
		val, err := json.Marshal(kv.Val)
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}

	buf.WriteString("}")
	return buf.Bytes(), nil
}
