package genesis

import (
	"encoding/hex"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/utils/threshold"
)

func TestForkV1_ValidatorTopicID(t *testing.T) {
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	t.Run("accepts valid key", func(t *testing.T) {
		topic := commons.ValidatorTopicID(sk.GetPublicKey().Serialize())
		t.Log("topic:", topic)
		require.Greater(t, len(topic), 0)
		require.Greater(t, len(topic[0]), 0)
		// require.Equal(t, 0, strings.Index(topic[0], "ssv.subnet."))
	})

	t.Run("deterministic", func(t *testing.T) {
		pkBytes, err := hex.DecodeString("892d99a9bf5c17ce12b962e659f66ba3a29504f10febfb08a521c4a35737c780f69373c95121fc8029def2b72e65918e")
		require.NoError(t, err)
		require.Equal(t, "63", commons.ValidatorTopicID(pkBytes)[0])
	})
}
