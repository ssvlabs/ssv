package forks

import "github.com/bloxapp/ssv/protocol/v1/message"

type Fork interface {
	EncodeSignedMsg(msg *message.SignedMessage) ([]byte, error)
	DecodeSignedMsg(data []byte) (*message.SignedMessage, error)
	Identifier(pk []byte, role message.RoleType) []byte
}
