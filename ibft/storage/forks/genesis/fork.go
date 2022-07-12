package genesis

import "github.com/bloxapp/ssv/protocol/v1/message"

// ForkGenesis is the genesis fork for controller
type ForkGenesis struct {
}

// Identifier returns the identifier of the fork
func (f ForkGenesis) Identifier(pk []byte, role message.RoleType) []byte {
	return message.NewIdentifier(pk, role)
}

// EncodeSignedMsg encodes signed message
func (f ForkGenesis) EncodeSignedMsg(msg *message.SignedMessage) ([]byte, error) {
	return msg.Encode()
}

// DecodeSignedMsg decodes signed message
func (f ForkGenesis) DecodeSignedMsg(data []byte) (*message.SignedMessage, error) {
	msgV1 := &message.SignedMessage{}
	err := msgV1.Decode(data)
	if err != nil {
		return nil, err
	}
	return msgV1, nil
}
