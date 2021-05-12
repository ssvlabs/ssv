package proto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignedMessage_DeepCopy(t *testing.T) {
	toCopy := &SignedMessage{
		Message: &Message{
			Type:           RoundState_Prepare,
			Round:          1,
			Lambda:         []byte("lambda"),
			PreviousLambda: []byte("prev lambda"),
			Value:          []byte("value"),
		},
		Signature: []byte{1, 2, 3, 4},
		SignerIds: []uint64{2},
	}

	copied, err := toCopy.DeepCopy()
	require.NoError(t, err)

	// test message
	root, err := toCopy.Message.SigningRoot()
	require.NoError(t, err)
	rootCopy, err := copied.Message.SigningRoot()
	require.NoError(t, err)
	require.EqualValues(t, rootCopy, root)

	require.EqualValues(t, toCopy.Signature, copied.Signature)
	require.EqualValues(t, toCopy.SignerIds, copied.SignerIds)
}

func TestSignedMessage_AggregateSig(t *testing.T) {
	secretKeys, _ := generateNodes(4)

	a, _ := signMsg(0, secretKeys[0], &Message{
		Type:           RoundState_Prepare,
		Round:          1,
		Lambda:         []byte("lambda"),
		PreviousLambda: []byte("prev lambda"),
		Value:          []byte("value"),
	})

	b, _ := signMsg(1, secretKeys[1], &Message{
		Type:           RoundState_Prepare,
		Round:          1,
		Lambda:         []byte("lambda"),
		PreviousLambda: []byte("prev lambda"),
		Value:          []byte("value"),
	})

	// simple aggregate
	require.NoError(t, a.Aggregate(b))
	require.EqualValues(t, []uint64{0, 1}, a.SignerIds)
	aggPk := secretKeys[0].GetPublicKey()
	aggPk.Add(secretKeys[1].GetPublicKey())
	res, err := a.VerifySig(aggPk)
	require.NoError(t, err)
	require.True(t, res)

	// double aggregate
	require.EqualError(t, a.Aggregate(b), "can't aggregate messages with similar signers")

	// aggregate different messages
	c, _ := signMsg(2, secretKeys[2], &Message{
		Type:           RoundState_Prepare,
		Round:          1,
		Lambda:         []byte("wrong lambda"),
		PreviousLambda: []byte("prev lambda"),
		Value:          []byte("value"),
	})
	require.EqualError(t, a.Aggregate(c), "can't aggregate different messages")
}
