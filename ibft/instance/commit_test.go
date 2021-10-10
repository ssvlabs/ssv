package ibft

import (
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
)

func newInMemDb() basedb.IDb {
	db, _ := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})
	return db
}

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
			agg, err := proto.AggregateMessages(test.msgs)
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
		state: &proto.State{
			Round:         threadsafe.Uint64(1),
			PreparedValue: threadsafe.Bytes(nil),
			PreparedRound: threadsafe.Uint64(0),
		},
	}

	// no decided msg
	_, err := instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "missing decided message")

	// set prepared state
	instance.State().PreparedRound.Set(1)
	instance.State().PreparedValue.Set([]byte("value"))

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

	instance.decidedMsg, err = proto.AggregateMessages(instance.CommitMessages.ReadOnlyMessagesByRound(3))
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
		state: &proto.State{
			Round:     threadsafe.Uint64(1),
			Lambda:    threadsafe.Bytes(nil),
			SeqNumber: threadsafe.Uint64(0),
		},
	}
	pipeline := instance.CommitMsgPipeline()
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, , add commit msg, upon commit msg, ", pipeline.Name())
}

func TestProcessLateCommitMsg(t *testing.T) {
	sks, _ := GenerateNodes(4)
	storage := collections.NewIbft(newInMemDb(), zap.L(), "attestation")

	var sigs []*proto.SignedMessage
	for i := 1; i < 4; i++ {
		sigs = append(sigs, SignMsg(t, uint64(i), sks[uint64(i)], &proto.Message{
			Type:   proto.RoundState_Commit,
			Round:  3,
			Lambda: []byte("Lambda_ATTESTER"),
			Value:  []byte("value"),
		}))
	}
	decided, err := proto.AggregateMessages(sigs)
	require.NoError(t, err)

	tests := []struct {
		name        string
		expectedErr string
		updated     bool
		msg         *proto.SignedMessage
	}{
		{
			"valid",
			"",
			true,
			SignMsg(t, 4, sks[4], &proto.Message{
				Type:   proto.RoundState_Commit,
				Round:  3,
				Lambda: []byte("Lambda_ATTESTER"),
				Value:  []byte("value"),
			}),
		},
		{
			"invalid",
			"could not aggregate commit message",
			false,
			func() *proto.SignedMessage {
				msg := SignMsg(t, 4, sks[4], &proto.Message{
					Type:   proto.RoundState_Commit,
					Round:  3,
					Lambda: []byte("Lambda_ATTESTER"),
					Value:  []byte("value"),
				})
				msg.Signature = []byte("dummy")
				return msg
			}(),
		},
		{
			"not found",
			"",
			false,
			SignMsg(t, 4, sks[4], &proto.Message{
				Type:   proto.RoundState_Commit,
				Round:  3,
				Lambda: []byte("xxx_ATTESTER"),
				Value:  []byte("value"),
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, storage.SaveDecided(decided))
			updated, err := ProcessLateCommitMsg(test.msg, &storage, "Lambda")
			if len(test.expectedErr) > 0 {
				require.NotNil(t, err)
				require.True(t, strings.Contains(err.Error(), test.expectedErr))
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.updated, updated)
		})
	}
}
