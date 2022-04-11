package message

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/pkg/errors"
)

// MsgType is the type of the message
type MsgType uint32

const (
	// SSVConsensusMsgType are all QBFT consensus related messages
	SSVConsensusMsgType MsgType = iota
	// SSVSyncMsgType are all QBFT sync messages
	SSVSyncMsgType
	// SSVPostConsensusMsgType are all partial signatures sent after consensus
	SSVPostConsensusMsgType
)

func (mt MsgType) String() string {
	switch mt {
	case SSVConsensusMsgType:
		return "consensus"
	case SSVPostConsensusMsgType:
		return "post-consensus"
	case SSVSyncMsgType:
		return "sync"
	default:
		return "unknown"
	}
}

// ValidatorPK is an eth2 validator public key
type ValidatorPK []byte

// MessageIDBelongs returns true if message ID belongs to validator
func (vid ValidatorPK) MessageIDBelongs(msgID Identifier) bool {
	toMatch := msgID[:len(vid)]
	return bytes.Equal(vid, toMatch)
}

// Identifier is used to identify and route messages to the right validator and DutyRunner
type Identifier []byte

// NewIdentifier creates a new Identifier
func NewIdentifier(pk []byte, role beacon.RoleType) Identifier {
	roleByts := make([]byte, 4)
	binary.LittleEndian.PutUint32(roleByts, uint32(role))
	return append(pk, roleByts...)
}

// GetRoleType extracts the role type from the id
func (msgID Identifier) GetRoleType() beacon.RoleType {
	if len(msgID) == 0 {
		return beacon.RoleTypeUnknown
	}
	roleByts := msgID[len(msgID)-4:]
	return beacon.RoleType(binary.LittleEndian.Uint32(roleByts))
}

// GetValidatorPK extracts the validator public key from the id
func (msgID Identifier) GetValidatorPK() ValidatorPK {
	if len(msgID) == 0 {
		return []byte{}
	}
	vpk := msgID[:len(msgID)-4]
	return ValidatorPK(vpk)
}

// String returns the string representation of the id
func (msgID Identifier) String() string {
	return hex.EncodeToString(msgID)
}

// SSVMessage is the main message passed within the SSV network, it can contain different types of messages (QBTF, Sync, etc.)
type SSVMessage struct {
	MsgType MsgType
	ID      Identifier
	Data    []byte
	//Version string
}

// GetType returns the msg type
func (msg *SSVMessage) GetType() MsgType {
	return msg.MsgType
}

// GetIdentifier returns a unique msg Identifier that is used to identify to which validator should the message be sent for processing
func (msg *SSVMessage) GetIdentifier() Identifier {
	return msg.ID
}

// GetData returns message Data as byte slice
func (msg *SSVMessage) GetData() []byte {
	return msg.Data
}

// Encode implements Encoder
func (msg *SSVMessage) Encode() ([]byte, error) {
	// TODO: change to SSZ encoding
	return json.Marshal(msg)
}

// Decode implements Encoder
func (msg *SSVMessage) Decode(data []byte) error {
	// TODO: change to SSZ encoding
	return json.Unmarshal(data, msg)
}

// MarshalJSON implements json.Marshaler
// all the top level values will be encoded to hex
func (msg *SSVMessage) MarshalJSON() ([]byte, error) {
	m := make(map[string]string)

	mt, err := json.Marshal(msg.MsgType)
	if err != nil {
		return nil, errors.Wrap(err, "MsgType marshaling failed")
	}
	m["type"] = hex.EncodeToString(mt)

	if msg.ID != nil {
		m["id"] = hex.EncodeToString(msg.ID)
	}

	if msg.Data != nil {
		data, err := json.Marshal(msg.Data)
		if err != nil {
			return nil, errors.Wrap(err, "Data marshaling failed")
		}
		m["Data"] = hex.EncodeToString(data)
	}
	return json.Marshal(m)
}

// UnmarshalJSON implements json.Unmarshaler
func (msg *SSVMessage) UnmarshalJSON(data []byte) error {
	m := make(map[string]string)
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, "could not unmarshal SSVMessage")
	}

	d, err := hex.DecodeString(m["type"])
	if err != nil {
		return errors.Wrap(err, "SSVMessage decode string failed")
	}
	if err := json.Unmarshal(d, &msg.MsgType); err != nil {
		return errors.Wrap(err, "could not unmarshal MsgType")
	}

	if val, ok := m["id"]; ok {
		id, err := hex.DecodeString(val)
		if err != nil {
			return errors.Wrap(err, "msg id decode string failed")
		}
		msg.ID = id
	}

	if val, ok := m["Data"]; ok {
		msg.Data = make([]byte, 0)
		data, err := hex.DecodeString(val)
		if err != nil {
			return errors.Wrap(err, "Data decode string failed")
		}
		if err := json.Unmarshal(data, &msg.Data); err != nil {
			msg.Data = nil
			return errors.Wrap(err, "could not unmarshal Data")
		}
	}
	return nil
}
