package ibft

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/bloxapp/ssv/ibft/consensus/weekday"

	"github.com/bloxapp/ssv/ibft/proto"

	"github.com/bloxapp/ssv/ibft/msgcont"

	"github.com/stretchr/testify/require"
)

func TestValidatePrepareMsg(t *testing.T) {
	sks, nodes := generateNodes(4)
	i := &Instance{
		prepareMessages:    msgcont.NewMessagesContainer(),
		prePrepareMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		state: &proto.State{
			Round:  1,
			Lambda: []byte("lambda"),
		},
		consensus: &weekday.Consensus{},
	}

	// test no valid pre-prepare msg
	msg := signMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.EqualError(t, i.validatePrepareMsg()(&msg), "no pre-prepare value found")

	// test invalid prepare value
	msg = signMsg(2, sks[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	i.prePrepareMessages.AddMessage(msg)

	msg = signMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte("invalid"),
	})
	require.EqualError(t, i.validatePrepareMsg()(&msg), "pre-prepare value not equal to prepare msg value")

	// test valid prepare value
	msg = signMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  2,
		Lambda: []byte("lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	require.NoError(t, i.validatePrepareMsg()(&msg))
}

func TestBatchedPrepareMsgsAndQuorum(t *testing.T) {
	_, nodes := generateNodes(4)
	i := &Instance{
		prepareMessages: msgcont.NewMessagesContainer(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		state: &proto.State{
			Round: 1,
		},
	}

	i.prepareMessages.AddMessage(proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 1,
	})
	i.prepareMessages.AddMessage(proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 2,
	})
	i.prepareMessages.AddMessage(proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  []byte("value"),
		},
		IbftId: 3,
	})
	i.prepareMessages.AddMessage(proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_Prepare,
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
