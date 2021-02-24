package testing

import (
	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/bloxapp/ssv/ibft/proto"
)

// SignMsg signs the given message by the given private key
func SignMsg(id uint64, secretKey *bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	signature, _ := msg.Sign(secretKey)
	return &proto.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}
}
