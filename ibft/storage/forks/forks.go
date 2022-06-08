package forks

import "github.com/bloxapp/ssv/protocol/v1/message"

// Fork is the interface for fork
type Fork interface {
	EncodeSignedMsg(msg *message.SignedMessage) ([]byte, error)
	DecodeSignedMsg(data []byte) (*message.SignedMessage, error)
	Identifier(pk []byte, role message.RoleType) []byte
}
