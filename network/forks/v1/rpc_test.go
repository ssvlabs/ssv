package v1

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestForkV1_RPC_Fork(t *testing.T) {
	v1Fork := New(100)

	t.Run("pre fork", func(t *testing.T) {
		require.EqualValues(t, forks.LegacyMsgStream, v1Fork.HighestDecidedStreamProtocol())
		require.EqualValues(t, forks.LegacyMsgStream, v1Fork.DecidedByRangeStreamProtocol())
		require.EqualValues(t, forks.LegacyMsgStream, v1Fork.LastChangeRoundStreamProtocol())
	})

	t.Run("post fork", func(t *testing.T) {
		v1Fork.SlotTick(100)
		require.EqualValues(t, forks.HighestDecidedStream, v1Fork.HighestDecidedStreamProtocol())
		require.EqualValues(t, forks.DecidedByRangeStream, v1Fork.DecidedByRangeStreamProtocol())
		require.EqualValues(t, forks.LastChangeRoundMsgStream, v1Fork.LastChangeRoundStreamProtocol())
	})
}
