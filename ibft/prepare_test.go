package ibft

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/types"
)

func TestBatchedPrepareMsgsAndQuorum(t *testing.T) {
	_, nodes := generateNodes(4)
	i := &iBFTInstance{
		prepareMessages: types.NewMessagesContainer(),
		params: &types.InstanceParams{
			ConsensusParams: types.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		state: &types.State{
			Round: 1,
		},
	}

	i.prepareMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 1,
	})
	i.prepareMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 2,
	})
	i.prepareMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 3,
	})
	i.prepareMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value2"),
		},
		IbftId: 4,
	})

	// test batch
	res := i.batchedPrepareMsgs(1)
	require.Len(t, res, 2)
	require.Len(t, res[hex.EncodeToString([]byte("value"))], 3)
	require.Len(t, res[hex.EncodeToString([]byte("value2"))], 1)

	// test valid quorum
	quorum, tt, n := i.prepareQuorum(1, []byte("value"))
	require.True(t, quorum)
	require.EqualValues(t, 3, tt)
	require.EqualValues(t, 4, n)

	// test invalid quorum
	quorum, tt, n = i.prepareQuorum(1, []byte("value2"))
	require.False(t, quorum)
	require.EqualValues(t, 1, tt)
	require.EqualValues(t, 4, n)
}
