package ibft

import (
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
)

func TestCommittedAggregatedMsg(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		CommitMessages: msgcontinmem.New(3),
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

	instance.decidedMsg = instance.aggregateMessages(instance.CommitMessages.ReadOnlyMessagesByRound(3))

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
		PrepareMessages: msgcontinmem.New(3),
		ValidatorShare:  &storage.Share{Committee: nodes, PublicKey: sks[1].GetPublicKey()},
		State: &proto.State{
			Round:     threadsafe.Uint64(1),
			Lambda:    threadsafe.Bytes(nil),
			SeqNumber: threadsafe.Uint64(0),
		},
	}
	pipeline := instance.commitMsgPipeline()
	require.EqualValues(t, "combination of: round, combination of: basic msg validation, type check, lambda, sequence, authorize, , add commit msg, upon commit msg, ", pipeline.Name())
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
	decided := AggregateMessages(zap.L(), sigs)

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
