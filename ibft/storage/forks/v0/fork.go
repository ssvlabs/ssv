package v0

import (
	"github.com/bloxapp/ssv/ibft/conversion"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/utils/format"
)

// ForkV0 is the genesis fork for controller
type ForkV0 struct {
}

// Identifier return the proper identifier
func (f *ForkV0) Identifier(pk []byte, role message.RoleType) []byte {
	return []byte(format.IdentifierFormat(pk, role.String())) // need to support same seed as v0 versions
}

// EncodeSignedMsg converts the message to v0 and encodes the message
func (f ForkV0) EncodeSignedMsg(msg *message.SignedMessage) ([]byte, error) {
	identifier := f.Identifier(msg.Message.Identifier.GetValidatorPK(), msg.Message.Identifier.GetRoleType())
	messageV0, err := conversion.ToSignedMessageV0(msg, identifier)
	if err != nil {
		return nil, err
	}
	return messageV0.Encode()
}

// DecodeSignedMsg decodes the v0 message and converts it to v1
func (f ForkV0) DecodeSignedMsg(data []byte) (*message.SignedMessage, error) {
	msgV0 := &proto.SignedMessage{}
	err := msgV0.Decode(data)
	if err != nil {
		return nil, err
	}
	return conversion.ToSignedMessageV1(msgV0)
}
