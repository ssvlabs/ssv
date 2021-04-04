package proto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundState_CanAcceptMsgsFromStage(t *testing.T) {
	tests := []struct {
		name           string
		currentStage   RoundState
		otherStage     RoundState
		expectedResult bool
	}{
		{
			"not started accepts pre-prepare",
			RoundState_NotStarted,
			RoundState_PrePrepare,
			true,
		},
		{
			"not started NOT accepting prepare",
			RoundState_NotStarted,
			RoundState_Prepare,
			false,
		},
		{
			"not started NOT accepting commit",
			RoundState_NotStarted,
			RoundState_Commit,
			false,
		},
		{
			"not started NOT accepting decided",
			RoundState_NotStarted,
			RoundState_Decided,
			false,
		},
		{
			"not started NOT accepting change round",
			RoundState_NotStarted,
			RoundState_ChangeRound,
			false,
		},
		{
			"pre-prepare NOT accepting not started",
			RoundState_PrePrepare,
			RoundState_NotStarted,
			false,
		},
		{
			"pre-prepare NOT accepting prepare",
			RoundState_PrePrepare,
			RoundState_Prepare,
			false,
		},
		{
			"pre-prepare NOT accepting commit",
			RoundState_PrePrepare,
			RoundState_Commit,
			false,
		},
		{
			"pre-prepare NOT accepting decided",
			RoundState_PrePrepare,
			RoundState_Decided,
			false,
		},
		{
			"pre-prepare accepting change round",
			RoundState_PrePrepare,
			RoundState_ChangeRound,
			true,
		},
		{
			"prepare NOT accepting pre-prepare",
			RoundState_Prepare,
			RoundState_PrePrepare,
			false,
		},
		{
			"prepare accepting prepare",
			RoundState_Prepare,
			RoundState_Prepare,
			true,
		},
		{
			"prepare accepting commit",
			RoundState_Prepare,
			RoundState_Commit,
			true,
		},
		{
			"prepare NOT accepting decided",
			RoundState_Prepare,
			RoundState_Decided,
			false,
		},
		{
			"prepare accepting change round",
			RoundState_Prepare,
			RoundState_ChangeRound,
			true,
		},
		{
			"decided NOT accepting pre-prepare",
			RoundState_Decided,
			RoundState_PrePrepare,
			false,
		},
		{
			"decided NOT accepting prepare",
			RoundState_Decided,
			RoundState_Prepare,
			false,
		},
		{
			"decided accepting commit",
			RoundState_Decided,
			RoundState_Commit,
			true,
		},
		{
			"decided NOT accepting decided",
			RoundState_Decided,
			RoundState_Decided,
			false,
		},
		{
			"decided NOT accepting change round",
			RoundState_Decided,
			RoundState_ChangeRound,
			false,
		},
		{
			"change round accepting pre-prepare",
			RoundState_ChangeRound,
			RoundState_PrePrepare,
			true,
		},
		{
			"change round NOT accepting prepare",
			RoundState_ChangeRound,
			RoundState_Prepare,
			false,
		},
		{
			"change round NOT accepting commit",
			RoundState_ChangeRound,
			RoundState_Commit,
			false,
		},
		{
			"change round NOT accepting decided",
			RoundState_ChangeRound,
			RoundState_Decided,
			false,
		},
		{
			"change round accepting change round",
			RoundState_ChangeRound,
			RoundState_ChangeRound,
			true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.EqualValues(t, test.expectedResult, test.currentStage.CanAcceptMsgsFromStage(test.otherStage))
		})
	}
}

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
