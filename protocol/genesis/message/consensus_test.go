package message_test

import (
	"sort"
	"testing"

	"github.com/ssvlabs/ssv/protocol/genesis/message"
	protocoltesting "github.com/ssvlabs/ssv/protocol/genesis/testing"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/stretchr/testify/require"
)

func TestAggregateSorting(t *testing.T) {
	uids := []genesisspectypes.OperatorID{genesisspectypes.OperatorID(1), genesisspectypes.OperatorID(2), genesisspectypes.OperatorID(3), genesisspectypes.OperatorID(4)}
	secretKeys, _ := protocoltesting.GenerateBLSKeys(uids...)

	identifier := []byte("pk")

	generateSignedMsg := func(operatorId genesisspectypes.OperatorID) *genesisspecqbft.SignedMessage {
		return protocoltesting.SignMsg(t, secretKeys, []genesisspectypes.OperatorID{operatorId}, &genesisspecqbft.Message{
			MsgType:    genesisspecqbft.CommitMsgType,
			Height:     0,
			Round:      1,
			Identifier: identifier,
		})
	}

	signedMessage := generateSignedMsg(1)
	for i := 2; i <= 4; i++ {
		sig := generateSignedMsg(genesisspectypes.OperatorID(i))
		require.NoError(t, message.Aggregate(signedMessage, sig))
	}

	sorted := sort.SliceIsSorted(signedMessage.Signers, func(i, j int) bool {
		return signedMessage.Signers[i] < signedMessage.Signers[j]
	})
	require.True(t, sorted)
}
