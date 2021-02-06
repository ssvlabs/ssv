package proto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignedMessage_AggregateSig(t *testing.T) {
	secretKeys, _ := generateNodes(4)

	a := signMsg(0, secretKeys[0], &Message{
		Type:           RoundState_Prepare,
		Round:          1,
		Lambda:         []byte("lambda"),
		PreviousLambda: []byte("prev lambda"),
		Value:          []byte("value"),
	})

	b := signMsg(1, secretKeys[1], &Message{
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
	c := signMsg(2, secretKeys[2], &Message{
		Type:           RoundState_Prepare,
		Round:          1,
		Lambda:         []byte("wrong lambda"),
		PreviousLambda: []byte("prev lambda"),
		Value:          []byte("value"),
	})
	require.EqualError(t, a.Aggregate(c), "can't aggregate different messages")
}
