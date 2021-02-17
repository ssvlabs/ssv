package proto

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
)

func generateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*Node) {
	bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

func signMsg(id uint64, secretKey *bls.SecretKey, msg *Message) *SignedMessage {
	signature, _ := msg.Sign(secretKey)
	return &SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}
}

func TestPubKeysById(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	params := &InstanceParams{
		IbftCommittee: nodes,
	}

	// test single
	pks, err := params.PubKeysByID([]uint64{0})
	require.NoError(t, err)
	require.Len(t, pks, 1)
	require.EqualValues(t, pks[0].Serialize(), secretKeys[0].GetPublicKey().Serialize())

	// test multiple
	pks, err = params.PubKeysByID([]uint64{0, 1})
	require.NoError(t, err)
	require.Len(t, pks, 2)
	require.EqualValues(t, pks[0].Serialize(), secretKeys[0].GetPublicKey().Serialize())
	require.EqualValues(t, pks[1].Serialize(), secretKeys[1].GetPublicKey().Serialize())

	// test multiple with invalid
	pks, err = params.PubKeysByID([]uint64{0, 5})
	require.EqualError(t, err, "pk for id not found")
}
