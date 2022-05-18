package instance

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, nil)
}

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
	msg1 := SignMsg(t, 1, sks[1], &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       []byte("value"),
	})
	msg2 := SignMsg(t, 2, sks[2], &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       []byte("value"),
	})
	msg3 := SignMsg(t, 3, sks[3], &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       []byte("value"),
	})

	tests := []struct {
		name            string
		msgs            []*message.SignedMessage
		expectedSigners []message.OperatorID
		expectedError   string
	}{
		{
			"valid 3 signatures",
			[]*message.SignedMessage{
				msg1, msg2, msg3,
			},
			[]message.OperatorID{1, 2, 3},
			"",
		},
		{
			"valid 2 signatures",
			[]*message.SignedMessage{
				msg1, msg2,
			},
			[]message.OperatorID{1, 2},
			"",
		},
		{
			"valid 1 signatures",
			[]*message.SignedMessage{
				msg1,
			},
			[]message.OperatorID{1},
			"",
		},
		{
			"no sigs return err",
			[]*message.SignedMessage{},
			[]message.OperatorID{},
			"could not aggregate decided messages, no msgs",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agg, err := AggregateMessages(test.msgs)
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.ElementsMatch(t, test.expectedSigners, agg.Signers)
			}
		})
	}
}

// TODO(nkryuchkov): fix this test
func TestCommittedAggregatedMsg(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		CommitMessages: inmem.New(3, 2),
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes},
		state:          &qbft.State{},
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(message.Round(0))

	// no decided msg
	_, err := instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "missing decided message")

	// set prepared state
	instance.State().PreparedRound.Store(message.Round(1))
	instance.State().PreparedValue.Store([]byte("value"))

	// test prepared but no committed msgs
	_, err = instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "missing decided message")

	// test valid aggregation
	consensusMessage := &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       []byte("value"),
	}

	commitData, err := consensusMessage.GetCommitData()
	require.NoError(t, err)

	instance.CommitMessages.AddMessage(SignMsg(t, 1, sks[1], consensusMessage), commitData.Data)
	instance.CommitMessages.AddMessage(SignMsg(t, 2, sks[2], consensusMessage), commitData.Data)
	instance.CommitMessages.AddMessage(SignMsg(t, 3, sks[3], consensusMessage), commitData.Data)

	instance.decidedMsg, err = AggregateMessages(instance.CommitMessages.ReadOnlyMessagesByRound(3))
	require.NoError(t, err)

	// test aggregation
	msg, err := instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []message.OperatorID{1, 2, 3}, msg.Signers)

	// test that doesn't aggregate different value
	m := &message.ConsensusMessage{
		MsgType:    message.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       []byte("value2"),
	}

	commitData, err = m.GetCommitData()
	require.NoError(t, err)

	instance.CommitMessages.AddMessage(SignMsg(t, 3, sks[3], m), commitData.Data)
	msg, err = instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []message.OperatorID{1, 2, 3}, msg.Signers)

	// test verification
	share := beacon.Share{Committee: nodes}
	require.NoError(t, share.VerifySignedMessage(msg, forksprotocol.V1ForkVersion.String()))
}

// TODO(nkryuchkov): fix this test
func TestCommitPipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: inmem.New(3, 2),
		ValidatorShare:  &beacon.Share{Committee: nodes, PublicKey: sks[1].GetPublicKey()},
		state:           &qbft.State{},
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(message.Round(0))

	instance.setFork(testingFork(instance))
	pipeline := instance.CommitMsgPipeline()
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, , add commit msg, upon commit msg, ", pipeline.Name())
}

func TestProcessLateCommitMsg(t *testing.T) {
	sks, _ := GenerateNodes(4)
	db := qbftstorage.NewQBFTStore(newInMemDb(), zap.L(), "attestation")

	share := beacon.Share{}
	share.PublicKey = sks[1].GetPublicKey()
	share.Committee = make(map[message.OperatorID]*beacon.Node, 4)
	identifier := format.IdentifierFormat(share.PublicKey.Serialize(), message.RoleTypeAttester.String())

	var sigs []*message.SignedMessage
	for i := 1; i < 4; i++ {
		sigs = append(sigs, SignMsg(t, uint64(i), sks[uint64(i)], &message.ConsensusMessage{
			Height:     2,
			MsgType:    message.CommitMsgType,
			Round:      3,
			Identifier: []byte(identifier),
			Data:       []byte("value"),
		}))
	}
	decided, err := AggregateMessages(sigs)
	require.NoError(t, err)

	tests := []struct {
		name        string
		expectedErr string
		updated     interface{}
		msg         *message.SignedMessage
	}{
		{
			"valid",
			"",
			struct{}{},
			SignMsg(t, 4, sks[4], &message.ConsensusMessage{
				Height:     message.Height(2),
				MsgType:    message.CommitMsgType,
				Round:      3,
				Identifier: []byte(identifier),
				Data:       []byte("value"),
			}),
		},
		{
			"invalid",
			"could not aggregate commit message",
			nil,
			func() *message.SignedMessage {
				msg := SignMsg(t, 4, sks[4], &message.ConsensusMessage{
					Height:     2,
					MsgType:    message.CommitMsgType,
					Round:      3,
					Identifier: []byte(identifier),
					Data:       []byte("value"),
				})
				msg.Signature = []byte("dummy")
				return msg
			}(),
		},
		{
			"not found",
			"",
			nil,
			SignMsg(t, 4, sks[4], &message.ConsensusMessage{
				Height:     message.Height(2),
				MsgType:    message.CommitMsgType,
				Round:      3,
				Identifier: []byte("xxx_ATTESTER"),
				Data:       []byte("value"),
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, db.SaveDecided(decided))
			updated, err := ProcessLateCommitMsg(test.msg, db, &share)
			if len(test.expectedErr) > 0 {
				require.NotNil(t, err)
				require.True(t, strings.Contains(err.Error(), test.expectedErr))
			} else {
				require.NoError(t, err)
			}
			if test.updated != nil {
				require.NotNil(t, updated)
			} else {
				require.Nil(t, updated)
			}
		})
	}
}

// AggregateMessages will aggregate given msgs or return error
func AggregateMessages(sigs []*message.SignedMessage) (*message.SignedMessage, error) {
	var decided *message.SignedMessage
	var err error
	for _, msg := range sigs {
		if decided == nil {
			decided = msg.DeepCopy()
			if err != nil {
				return nil, errors.Wrap(err, "could not copy message")
			}
		} else {
			if err := decided.Aggregate(msg); err != nil {
				return nil, errors.Wrap(err, "could not aggregate message")
			}
		}
	}

	if decided == nil {
		return nil, errors.New("could not aggregate decided messages, no msgs")
	}

	return decided, nil
}

func commitDataToBytes(input *message.CommitData) []byte {
	ret, _ := json.Marshal(input)
	return ret
}
