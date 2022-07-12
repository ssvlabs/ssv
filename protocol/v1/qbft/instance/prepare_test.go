package instance

import (
	"go.uber.org/zap"
	"testing"

	"github.com/stretchr/testify/require"

	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
)

func TestPreparedAggregatedMsg(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		containersMap: map[qbftspec.MessageType]msgcont.MessageContainer{
			qbftspec.PrepareMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			NodeID:    1,
		},
		state:  &qbft.State{},
		Logger: zap.L(),
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(message.Round(0))

	// not prepared
	_, err := instance.PreparedAggregatedMsg()
	require.EqualError(t, err, "state not prepared")

	// set prepared state
	instance.State().PreparedRound.Store(message.Round(1))
	instance.State().PreparedValue.Store([]byte("value"))

	// test prepared but no msgs
	_, err = instance.PreparedAggregatedMsg()
	require.EqualError(t, err, "no prepare msgs")

	// test valid aggregation
	consensusMessage1 := &message.ConsensusMessage{
		MsgType:    message.PrepareMsgType,
		Round:      1,
		Identifier: []byte("Lambda"),
		Data:       prepareDataToBytes(t, &message.PrepareData{Data: []byte("value")}),
	}

	prepareData, err := consensusMessage1.GetPrepareData()
	require.NoError(t, err)

	instance.containersMap[qbftspec.PrepareMsgType].AddMessage(SignMsg(t, 1, sks[1], consensusMessage1), prepareData.Data)
	instance.containersMap[qbftspec.PrepareMsgType].AddMessage(SignMsg(t, 2, sks[2], consensusMessage1), prepareData.Data)
	instance.containersMap[qbftspec.PrepareMsgType].AddMessage(SignMsg(t, 3, sks[3], consensusMessage1), prepareData.Data)

	// test aggregation
	msg, err := instance.PreparedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []message.OperatorID{1, 2, 3}, msg.Signers)

	// test that doesn't aggregate different value
	consensusMessage2 := &message.ConsensusMessage{
		MsgType:    message.PrepareMsgType,
		Round:      1,
		Identifier: []byte("Lambda"),
		Data:       prepareDataToBytes(t, &message.PrepareData{Data: []byte("value2")}),
	}
	instance.containersMap[qbftspec.PrepareMsgType].AddMessage(SignMsg(t, 4, sks[4], consensusMessage2), prepareData.Data)
	msg, err = instance.PreparedAggregatedMsg()
	require.NoError(t, err)
	require.ElementsMatch(t, []message.OperatorID{1, 2, 3}, msg.Signers)
}

func TestPreparePipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		containersMap: map[qbftspec.MessageType]msgcont.MessageContainer{
			qbftspec.PrepareMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			NodeID:    1,
			PublicKey: sks[1].GetPublicKey(),
		},
		state: &qbft.State{},
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.Identifier.Store(message.Identifier(nil))
	instance.state.Height.Store(message.Height(0))

	instance.fork = testingFork(instance)
	pipeline := instance.PrepareMsgPipeline()
	// TODO: fix bad-looking name
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, , add prepare msg, if first pipeline non error, continue to second, ", pipeline.Name())
}
