package spectesting

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/bloxapp/ssv/ibft/proto"
)

// SignMsg signs the given message by the given private key
func SignMsg(t *testing.T, id uint64, skByts []byte, msg *proto.Message) *proto.SignedMessage {
	require.NoError(t, bls.Init(bls.BLS12_381))

	// add validator PK to all msgs
	msg.ValidatorPk = fixtures.RefPk

	sk := &bls.SecretKey{}
	require.NoError(t, sk.Deserialize(skByts))

	signature, err := msg.Sign(sk)
	require.NoError(t, err)
	return &proto.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}
}
