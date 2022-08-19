package instance

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, nil)
}

func TestAggregatedMsg(t *testing.T) {
	sks, _, operatorIds, _ := GenerateNodes(4)
	commitData, err := (&specqbft.CommitData{Data: []byte("value")}).Encode()
	require.NoError(t, err)
	msg1 := SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       commitData,
	})
	msg2 := SignMsg(t, operatorIds[1:2], sks[operatorIds[1]], &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       commitData,
	})
	msg3 := SignMsg(t, operatorIds[2:3], sks[operatorIds[2]], &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       commitData,
	})

	tests := []struct {
		name            string
		msgs            []*specqbft.SignedMessage
		expectedSigners []spectypes.OperatorID
		expectedError   string
	}{
		{
			"valid 3 signatures",
			[]*specqbft.SignedMessage{
				msg1, msg2, msg3,
			},
			operatorIds[:3],
			"",
		},
		{
			"valid 2 signatures",
			[]*specqbft.SignedMessage{
				msg1, msg2,
			},
			operatorIds[:2],
			"",
		},
		{
			"valid 1 signatures",
			[]*specqbft.SignedMessage{
				msg1,
			},
			operatorIds[:1],
			"",
		},
		{
			"no sigs return err",
			[]*specqbft.SignedMessage{},
			[]spectypes.OperatorID{},
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

func TestCommittedAggregatedMsg(t *testing.T) {
	t.Run("v0", func(t *testing.T) {
		committedAggregatedMsg(t)
	})
	t.Run("v1", func(t *testing.T) {
		committedAggregatedMsg(t)
	})
}

func committedAggregatedMsg(t *testing.T) {
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)
	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.CommitMsgType: inmem.New(3, 2),
		},
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes, OperatorIds: shareOperatorIds},
		state: &qbft.State{
			Round:         qbft.NewRound(specqbft.Round(1)),
			PreparedValue: qbft.NewByteValue([]byte(nil)),
			PreparedRound: qbft.NewRound(specqbft.Round(0)),
		},
	}

	// no decided msg
	_, err := instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "missing decided message")

	// set prepared state
	instance.State().PreparedRound.Store(specqbft.Round(1))
	instance.State().PreparedValue.Store([]byte("value"))

	// test prepared but no committed msgs
	_, err = instance.CommittedAggregatedMsg()
	require.EqualError(t, err, "missing decided message")

	// test valid aggregation
	consensusMessage := &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value")}),
	}

	commitData, err := consensusMessage.GetCommitData()
	require.NoError(t, err)

	instance.containersMap[specqbft.CommitMsgType].AddMessage(SignMsg(t, operatorIds[:1], sks[operatorIds[0]], consensusMessage), commitData.Data)
	instance.containersMap[specqbft.CommitMsgType].AddMessage(SignMsg(t, operatorIds[1:2], sks[operatorIds[1]], consensusMessage), commitData.Data)
	instance.containersMap[specqbft.CommitMsgType].AddMessage(SignMsg(t, operatorIds[2:3], sks[operatorIds[2]], consensusMessage), commitData.Data)

	instance.decidedMsg, err = AggregateMessages(instance.containersMap[specqbft.CommitMsgType].ReadOnlyMessagesByRound(3))
	require.NoError(t, err)

	// test aggregation
	msg, err := instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, operatorIds[:3], msg.Signers)

	// test that doesn't aggregate different value
	m := &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       commitDataToBytes(t, &specqbft.CommitData{Data: []byte("value2")}),
	}

	commitData, err = m.GetCommitData()
	require.NoError(t, err)

	instance.containersMap[specqbft.CommitMsgType].AddMessage(SignMsg(t, operatorIds[2:3], sks[operatorIds[2]], m), commitData.Data)
	msg, err = instance.CommittedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, operatorIds[:3], msg.Signers)

	// test verification
	share := beacon.Share{Committee: nodes}
	require.NoError(t, share.VerifySignedMessage(msg))
}

func TestCommitPipeline(t *testing.T) {
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: inmem.New(3, 2),
		},
		ValidatorShare: &beacon.Share{Committee: nodes, PublicKey: sks[operatorIds[0]].GetPublicKey(), OperatorIds: shareOperatorIds},
		state:          &qbft.State{},
	}

	instance.state.Round.Store(specqbft.Round(1))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(specqbft.Round(0))

	instance.setFork(testingFork(instance))
	pipeline := instance.CommitMsgPipeline()
	require.EqualValues(t, "combination of: validate proposal, combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, , add commit msg, , upon commit msg, ", pipeline.Name())
}

// AggregateMessages will aggregate given msgs or return error
func AggregateMessages(sigs []*specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	var decided *specqbft.SignedMessage
	var err error
	for _, msg := range sigs {
		if decided == nil {
			decided = msg.DeepCopy()
			if err != nil {
				return nil, errors.Wrap(err, "could not copy message")
			}
		} else {
			if err := message.Aggregate(decided, msg); err != nil {
				return nil, errors.Wrap(err, "could not aggregate message")
			}
		}
	}

	if decided == nil {
		return nil, errors.New("could not aggregate decided messages, no msgs")
	}

	return decided, nil
}

func commitDataToBytes(t *testing.T, input *specqbft.CommitData) []byte {
	ret, err := input.Encode()
	require.NoError(t, err)
	return ret
}
