package ibft

import (
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/bloxapp/ssv/validator/storage"
	"testing"

	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
)

func TestAggregatedMsg(t *testing.T) {
	sks, _ := GenerateNodes(4)
	msg1 := SignMsg(t, 1, sks[1], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	})
	msg2 := SignMsg(t, 2, sks[2], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	})
	msg3 := SignMsg(t, 3, sks[3], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	})
	msgDiff := SignMsg(t, 4, sks[4], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  2,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	})

	tests := []struct {
		name            string
		msgs            []*proto.SignedMessage
		expectedSigners []uint64
		expectedError   string
	}{
		{
			"valid 3 signatures",
			[]*proto.SignedMessage{
				msg1, msg2, msg3,
			},
			[]uint64{1, 2, 3},
			"",
		},
		{
			"valid 2 signatures",
			[]*proto.SignedMessage{
				msg1, msg2,
			},
			[]uint64{1, 2},
			"",
		},
		{
			"valid 1 signatures",
			[]*proto.SignedMessage{
				msg1,
			},
			[]uint64{1},
			"",
		},
		{
			"no sigs return err",
			[]*proto.SignedMessage{},
			[]uint64{},
			"could not aggregate decided messages, no msgs",
		},
		{
			"different msgs, can't aggregate",
			[]*proto.SignedMessage{msg1, msgDiff},
			[]uint64{},
			"could not aggregate message: can't aggregate different messages",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance := &Instance{}
			agg, err := instance.aggregateMessages(test.msgs)
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.ElementsMatch(t, test.expectedSigners, agg.SignerIds)
			}
		})
	}
}

func TestCommittedAggregatedMsg(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		CommitMessages: msgcontinmem.New(3, 2),
		Config:         proto.DefaultConsensusParams(),
		ValidatorShare: &storage.Share{Committee: nodes},
		State: &proto.State{
			Round:         threadsafe.Uint64(1),
			PreparedValue: threadsafe.Bytes(nil),
			PreparedRound: threadsafe.Uint64(0),
		},
	}

	// no decided msg
	_, err := instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "missing decided message")

	// set prepared state
	instance.State.PreparedRound.Set(1)
	instance.State.PreparedValue.Set([]byte("value"))

	// test prepared but no committed msgs
	_, err = instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "missing decided message")

	// test valid aggregation
	instance.CommitMessages.AddMessage(SignMsg(t, 1, sks[1], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.CommitMessages.AddMessage(SignMsg(t, 2, sks[2], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.CommitMessages.AddMessage(SignMsg(t, 3, sks[3], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))

	instance.decidedMsg, err = instance.aggregateMessages(instance.CommitMessages.ReadOnlyMessagesByRound(3))
	require.NoError(t, err)

	// test aggregation
	msg, err := instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2, 3}, msg.SignerIds)

	// test that doesn't aggregate different value
	instance.CommitMessages.AddMessage(SignMsg(t, 3, sks[3], &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  3,
		Lambda: []byte("Lambda"),
		Value:  []byte("value2"),
	}))
	msg, err = instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []uint64{1, 2, 3}, msg.SignerIds)

	// test verification
	share := storage.Share{Committee: nodes}
	require.NoError(t, share.VerifySignedMessage(msg))
}

func TestCommitPipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3, 2),
		ValidatorShare:  &storage.Share{Committee: nodes, PublicKey: sks[1].GetPublicKey()},
		State: &proto.State{
			Round:     threadsafe.Uint64(1),
			Lambda:    threadsafe.Bytes(nil),
			SeqNumber: threadsafe.Uint64(0),
		},
	}
	pipeline := instance.commitMsgPipeline()
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, , add commit msg, upon commit msg, ", pipeline.Name())
}
