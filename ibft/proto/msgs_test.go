package proto

import (
	"github.com/herumi/bls-eth-go-binary/bls"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignedMessage_DeepCopy(t *testing.T) {
	toCopy := &SignedMessage{
		Message: &Message{
			Type:   RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
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
		Type:   RoundState_Prepare,
		Round:  1,
		Lambda: []byte("lambda"),
		Value:  []byte("value"),
	})

	b, _ := signMsg(1, secretKeys[1], &Message{
		Type:   RoundState_Prepare,
		Round:  1,
		Lambda: []byte("lambda"),
		Value:  []byte("value"),
	})

	// simple aggregate
	t.Run("simple aggregate", func(t *testing.T) {
		require.NoError(t, a.Aggregate(b))
		require.EqualValues(t, []uint64{0, 1}, a.SignerIds)
		aggPk := secretKeys[0].GetPublicKey()
		aggPk.Add(secretKeys[1].GetPublicKey())
		res, err := a.VerifySig(aggPk)
		require.NoError(t, err)
		require.True(t, res)
	})

	t.Run("duplicate aggregate", func(t *testing.T) {
		require.EqualError(t, a.Aggregate(b), "can't aggregate messages with similar signers")
	})

	t.Run("aggregate different messages", func(t *testing.T) {
		c, _ := signMsg(2, secretKeys[2], &Message{
			Type:           RoundState_Prepare,
			Round:          1,
			Lambda:         []byte("wrong lambda"),
			Value:          []byte("value"),
		})
		require.EqualError(t, a.Aggregate(c), "can't aggregate different messages")
	})
}

func TestSignedMessage_VerifyAggregatedSig(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	params := &InstanceParams{
		ConsensusParams: DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}
	tests := []struct {
		name          string
		msgs          *Message
		signers       []uint64
		expectedError string
	}{
		{
			"simple single sig",
			&Message{
				Type:   RoundState_Prepare,
				Round:  1,
				Lambda: []byte("lambda"),
				Value:  []byte("value"),
			},
			[]uint64{1},
			"",
		},
		{
			"valid aggregated sig",
			&Message{
				Type:   RoundState_Prepare,
				Round:  1,
				Lambda: []byte("lambda"),
				Value:  []byte("value"),
			},
			[]uint64{1, 2, 3},
			"",
		},
		{
			"invalid aggregated sig, non unique signers",
			&Message{
				Type:   RoundState_Prepare,
				Round:  1,
				Lambda: []byte("lambda"),
				Value:  []byte("value"),
			},
			[]uint64{1, 1, 3},
			"signers are not unique",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			aggSignedMsg := &SignedMessage{
				Message:   test.msgs,
				Signature: nil,
				SignerIds: test.signers,
			}

			// aggregate
			var aggSig *bls.Sign
			for _, signerID := range test.signers {
				_, sig := signMsg(signerID, secretKeys[signerID], test.msgs)
				if aggSig == nil {
					aggSig = sig
				} else {
					aggSig.Add(sig)
				}
			}
			aggSignedMsg.Signature = aggSig.Serialize()

			err := params.VerifySignedMessage(aggSignedMsg)
			if len(test.expectedError) == 0 {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, test.expectedError)
			}

		})
	}
}

func TestVerifyUniqueSigners(t *testing.T) {
	tests := []struct {
		name      string
		signerIds []uint64
		err       string
	}{
		{
			"valid list of signers",
			[]uint64{1, 2, 3},
			"",
		},
		{
			"duplicated signers",
			[]uint64{1, 2, 2},
			"signers are not unique",
		},
		{
			"no signers",
			[]uint64{},
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifyUniqueSigners(test.signerIds)
			if len(test.err) > 0 {
				require.EqualError(t, err, test.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
