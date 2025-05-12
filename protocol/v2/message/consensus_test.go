package message_test

import (
	"crypto/rsa"
	"sort"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/message"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

func TestAggregateSorting(t *testing.T) {
	uids := []spectypes.OperatorID{spectypes.OperatorID(1), spectypes.OperatorID(2), spectypes.OperatorID(3), spectypes.OperatorID(4)}
	secretKeys, _ := protocoltesting.GenerateOperatorSigner(uids...)

	identifier := []byte("pk")

	generateSignedMsg := func(operatorId spectypes.OperatorID) *spectypes.SignedSSVMessage {
		return protocoltesting.SignMsg(t, []*rsa.PrivateKey{secretKeys[operatorId-1]}, []spectypes.OperatorID{operatorId}, &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     0,
			Round:      1,
			Identifier: identifier,
		})
	}

	signedMessage := generateSignedMsg(1)
	for i := 2; i <= 4; i++ {
		sig := generateSignedMsg(spectypes.OperatorID(i))
		require.NoError(t, message.Aggregate(signedMessage, sig))
	}

	sorted := sort.SliceIsSorted(signedMessage.OperatorIDs, func(i, j int) bool {
		return signedMessage.OperatorIDs[i] < signedMessage.OperatorIDs[j]
	})

	require.True(t, sorted)
}
