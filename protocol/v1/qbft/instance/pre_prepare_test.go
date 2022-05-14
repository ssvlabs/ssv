package instance

import (
	"strconv"
	"testing"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/constant"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/deterministic"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
)

func TestJustifyPrePrepareAfterChangeRoundPrepared(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	value := []byte(time.Now().Weekday().String())
	instance := &Instance{
		PrePrepareMessages:  inmem.New(3, 2),
		ChangeRoundMessages: inmem.New(3, 2),
		Config:              qbft.DefaultConsensusParams(),
		state:               &qbft.State{},
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			NodeID:    1,
		},
		Logger: zaptest.NewLogger(t),
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.Identifier.Store([]byte("Lambda"))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(message.Round(0))

	consensusMessage := &message.ConsensusMessage{
		MsgType:    message.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("Lambda"),
		Data: changeRoundDataToBytes(&message.RoundChangeData{
			Round:         1,
			PreparedValue: value,
		}),
	}

	roundChangeData, err := consensusMessage.GetRoundChangeData()
	require.NoError(t, err)

	t.Run("not quorum, not justified", func(t *testing.T) {
		// change round no quorum
		msg := SignMsg(t, 1, secretKeys[1], consensusMessage)
		instance.ChangeRoundMessages.AddMessage(msg, roundChangeData.PreparedValue)

		// no quorum achieved, err
		msg = SignMsg(t, 1, secretKeys[1], &message.ConsensusMessage{
			MsgType:    message.ProposalMsgType,
			Round:      2,
			Identifier: []byte("Lambda"),
			Data:       value,
		})
		instance.PrePrepareMessages.AddMessage(msg, roundChangeData.PreparedValue)
		err := instance.JustifyPrePrepare(2, value)
		require.EqualError(t, err, "no change round quorum")
	})

	t.Run("change round quorum, justified", func(t *testing.T) {
		// test justified change round
		msg := SignMsg(t, 2, secretKeys[2], consensusMessage)
		instance.ChangeRoundMessages.AddMessage(msg, roundChangeData.PreparedValue)
		msg = SignMsg(t, 3, secretKeys[3], consensusMessage)
		instance.ChangeRoundMessages.AddMessage(msg, roundChangeData.PreparedValue)

		err := instance.JustifyPrePrepare(2, value)
		require.NoError(t, err)
	})

	t.Run("wrong value, unjustified", func(t *testing.T) {
		err := instance.JustifyPrePrepare(2, []byte("wrong value"))
		require.EqualError(t, err, "preparedValue different than highest prepared")
	})
}

func TestJustifyPrePrepareAfterChangeRoundNoPrepare(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	instance := &Instance{
		PrePrepareMessages:  inmem.New(3, 2),
		PrepareMessages:     inmem.New(3, 2),
		ChangeRoundMessages: inmem.New(3, 2),
		Config:              qbft.DefaultConsensusParams(),
		state:               &qbft.State{},
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			NodeID:    1,
		},
		Logger: zaptest.NewLogger(t),
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.Identifier.Store([]byte("Lambda"))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(message.Round(0))

	consensusMessage := &message.ConsensusMessage{
		MsgType:    message.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("Lambda"),
		Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
	}

	roundChangeData, err := consensusMessage.GetRoundChangeData()
	require.NoError(t, err)

	t.Run("no change round quorum, not justified", func(t *testing.T) {
		// change round no quorum
		msg := SignMsg(t, 1, secretKeys[1], consensusMessage)
		instance.ChangeRoundMessages.AddMessage(msg, roundChangeData.PreparedValue)

		msg = SignMsg(t, 2, secretKeys[2], consensusMessage)
		instance.ChangeRoundMessages.AddMessage(msg, roundChangeData.PreparedValue)

		// no quorum achieved, can't justify
		err := instance.JustifyPrePrepare(2, nil)
		require.EqualError(t, err, "no change round quorum")
	})

	t.Run("change round quorum, justified", func(t *testing.T) {
		// test justified change round
		msg := SignMsg(t, 3, secretKeys[3], consensusMessage)
		instance.ChangeRoundMessages.AddMessage(msg, roundChangeData.PreparedValue)

		// quorum achieved, can justify
		err := instance.JustifyPrePrepare(2, nil)
		require.NoError(t, err)
	})

	t.Run("any value can be in pre-prepare", func(t *testing.T) {
		require.NoError(t, instance.JustifyPrePrepare(2, []byte("wrong value")))
	})
}

func TestUponPrePrepareHappyFlow(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	leader, err := deterministic.New(append([]byte{1, 2, 3, 2, 5, 6, 1, 1}, []byte(strconv.FormatUint(1, 10))...), 4)
	require.NoError(t, err)

	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)
	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)

	instance := &Instance{
		PrePrepareMessages: inmem.New(3, 2),
		PrepareMessages:    inmem.New(3, 2),
		Config:             qbft.DefaultConsensusParams(),
		state:              &qbft.State{},
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			NodeID:    1,
			PublicKey: secretKeys[1].GetPublicKey(),
		},
		Logger:         zaptest.NewLogger(t),
		network:        network,
		LeaderSelector: leader,
		signer:         newTestSigner(),
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.Identifier.Store([]byte("Lambda"))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(message.Round(0))
	instance.state.Height.Store(message.Height(0))
	instance.state.Stage.Store(int32(qbft.RoundState_NotStarted))

	// test happy flow
	msg := SignMsg(t, 1, secretKeys[1], &message.ConsensusMessage{
		MsgType:    message.ProposalMsgType,
		Round:      1,
		Identifier: []byte("Lambda"),
		Data:       []byte(time.Now().Weekday().String()),
	})
	require.NoError(t, instance.PrePrepareMsgPipeline().Run(msg))
	msgs := instance.PrePrepareMessages.ReadOnlyMessagesByRound(1)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0])
	require.True(t, instance.State().Stage.Load() == int32(proto.RoundState_PrePrepare))

	// return nil if another pre-prepare received.
	require.NoError(t, instance.UponPrePrepareMsg().Run(msg))
}

func TestInstance_JustifyPrePrepare(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)

	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)
	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)

	instance := &Instance{
		ChangeRoundMessages: inmem.New(3, 2),
		Config:              qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			NodeID:    1,
		},
		state:   &qbft.State{},
		network: network,
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(message.Round(0))

	require.NoError(t, instance.JustifyPrePrepare(1, nil))

	// try to justify round 2 without round change
	instance.State().Round.Store(message.Round(2))
	err = instance.JustifyPrePrepare(2, nil)
	require.EqualError(t, err, "no change round quorum")

	// test no change round quorum
	msg := &message.ConsensusMessage{
		MsgType:    message.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("lambdas"),
		Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
	}
	roundChangeData, err := msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.ChangeRoundMessages.AddMessage(SignMsg(t, 1, secretKeys[1], msg), roundChangeData.PreparedValue)

	msg = &message.ConsensusMessage{
		MsgType:    message.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("lambdas"),
		Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
	}
	roundChangeData, err = msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.ChangeRoundMessages.AddMessage(SignMsg(t, 2, secretKeys[2], msg), roundChangeData.PreparedValue)

	err = instance.JustifyPrePrepare(2, nil)
	require.EqualError(t, err, "no change round quorum")

	// test with quorum of change round
	msg = &message.ConsensusMessage{
		MsgType:    message.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("lambdas"),
		Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
	}
	roundChangeData, err = msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.ChangeRoundMessages.AddMessage(SignMsg(t, 3, secretKeys[3], msg), roundChangeData.PreparedValue)

	err = instance.JustifyPrePrepare(2, nil)
	require.NoError(t, err)
}

func TestPrePreparePipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: inmem.New(3, 2),
		Config:          qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			NodeID:    1,
			PublicKey: sks[1].GetPublicKey(),
		},
		state:          &qbft.State{},
		LeaderSelector: &constant.Constant{LeaderIndex: 1},
	}

	instance.state.Round.Store(message.Round(1))
	instance.state.Identifier.Store([]byte("Lambda"))
	instance.state.Height.Store(message.Height(0))

	pipeline := instance.PrePrepareMsgPipeline()
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, validate pre-prepare, , add pre-prepare msg, if first pipeline non error, continue to second, ", pipeline.Name())
}

type testSigner struct {
}

func newTestSigner() beacon.Signer {
	return &testSigner{}
}

func (s *testSigner) AddShare(shareKey *bls.SecretKey) error {
	return nil
}

func (s *testSigner) SignIBFTMessage(message *message.ConsensusMessage, pk []byte, forkVersion string) ([]byte, error) {
	return nil, nil
}

func (s *testSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}
