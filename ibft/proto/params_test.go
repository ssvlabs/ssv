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

func signMsg(id uint64, secretKey *bls.SecretKey, msg *Message) (*SignedMessage, *bls.Sign) {
	signature, _ := msg.Sign(secretKey)
	return &SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}, signature
}

func TestInstanceParams_ThresholdSize(t *testing.T) {
	for i := 1; i < 50; i++ {
		p := &InstanceParams{IbftCommittee: make(map[uint64]*Node)}
		// populate
		for j := 1; j <= 3*i+1; j++ {
			p.IbftCommittee[uint64(j)] = &Node{}
		}
		require.EqualValues(t, 2*i+1, p.ThresholdSize())
	}
}

func TestPubKeysById(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	params := &InstanceParams{
		IbftCommittee: nodes,
	}

	t.Run("test single", func(t *testing.T) {
		pks, err := params.PubKeysByID([]uint64{0})
		require.NoError(t, err)
		require.Len(t, pks, 1)
		require.EqualValues(t, pks[0].Serialize(), secretKeys[0].GetPublicKey().Serialize())
	})

	t.Run("test multiple", func(t *testing.T) {
		pks, err := params.PubKeysByID([]uint64{0, 1})
		require.NoError(t, err)
		require.Len(t, pks, 2)
		require.EqualValues(t, pks[0].Serialize(), secretKeys[0].GetPublicKey().Serialize())
		require.EqualValues(t, pks[1].Serialize(), secretKeys[1].GetPublicKey().Serialize())
	})

	t.Run("test multiple with invalid", func(t *testing.T) {
		_, err := params.PubKeysByID([]uint64{0, 5})
		require.EqualError(t, err, "pk for id not found")
	})
}

func TestVerifySignedMsg(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	params := &InstanceParams{
		IbftCommittee: nodes,
	}

	msg := &Message{
		Type:      RoundState_Decided,
		Round:     1,
		Lambda:    []byte{1, 2, 3, 4},
		SeqNumber: 1,
	}
	aggMessage, aggregated := signMsg(1, secretKeys[1], msg)
	_, sig2 := signMsg(2, secretKeys[2], msg)
	aggregated.Add(sig2)
	aggMessage.Signature = aggregated.Serialize()
	aggMessage.SignerIds = []uint64{1, 2}

	require.NoError(t, params.VerifySignedMessage(aggMessage))
	aggMessage.SignerIds = []uint64{1, 3}
	require.EqualError(t, params.VerifySignedMessage(aggMessage), "could not verify message signature")
}
