package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"github.com/bloxapp/ssv/beacon"
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
	roleByts := msgID[len(msgID)-4:]
	return beacon.RoleType(binary.LittleEndian.Uint32(roleByts))
}

// GetValidatorPK extracts the validator public key from the id
func (msgID Identifier) GetValidatorPK() ValidatorPK {
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
}

// GetType returns the msg type
func (msg *SSVMessage) GetType() MsgType {
	return msg.MsgType
}

// GetID returns a unique msg ID that is used to identify to which validator should the message be sent for processing
func (msg *SSVMessage) GetID() Identifier {
	return msg.ID
}

// GetData returns message Data as byte slice
func (msg *SSVMessage) GetData() []byte {
	return msg.Data
}
