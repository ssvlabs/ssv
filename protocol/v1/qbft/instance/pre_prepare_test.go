package instance

import (
	"encoding/json"
	"testing"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/constant"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/roundrobin"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
)

func TestJustifyPrePrepareAfterChangeRoundPrepared(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	value, err := (&specqbft.ProposalData{Data: []byte(time.Now().Weekday().String())}).Encode()
	require.NoError(t, err)
	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.ProposalMsgType:    inmem.New(3, 2),
			specqbft.RoundChangeMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		state:  &qbft.State{},
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			OperatorIds: shareOperatorIds,
		},
		Logger: zaptest.NewLogger(t),
	}

	instance.state.Round.Store(specqbft.Round(1))
	instance.state.Identifier.Store(spectypes.NewMsgID([]byte("Lambda"), spectypes.BNRoleAttester))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(specqbft.Round(0))

	consensusMessage := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("Lambda"),
		Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
			PreparedRound: 1,
			PreparedValue: value,
		}),
	}

	roundChangeData, err := consensusMessage.GetRoundChangeData()
	require.NoError(t, err)

	t.Run("not quorum, not justified", func(t *testing.T) {
		// change round no quorum
		msg := SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], consensusMessage)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		// no quorum achieved, err
		msg = SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Round:      2,
			Identifier: []byte("Lambda"),
			Data:       value,
		})
		instance.containersMap[specqbft.ProposalMsgType].AddMessage(msg, roundChangeData.PreparedValue)
		err := instance.JustifyPrePrepare(2, value)
		require.EqualError(t, err, "no change round quorum")
	})

	t.Run("change round quorum, justified", func(t *testing.T) {
		// test justified change round
		msg := SignMsg(t, operatorIds[1:2], secretKeys[operatorIds[1]], consensusMessage)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)
		msg = SignMsg(t, operatorIds[2:3], secretKeys[operatorIds[2]], consensusMessage)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		err := instance.JustifyPrePrepare(2, value)
		require.NoError(t, err)
	})

	t.Run("wrong value, unjustified", func(t *testing.T) {
		err := instance.JustifyPrePrepare(2, []byte("wrong value"))
		require.EqualError(t, err, "preparedValue different than highest prepared")
	})
}

func TestJustifyPrePrepareAfterChangeRoundNoPrepare(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.ProposalMsgType:    inmem.New(3, 2),
			specqbft.PrepareMsgType:     inmem.New(3, 2),
			specqbft.RoundChangeMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		state:  &qbft.State{},
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			OperatorIds: shareOperatorIds,
		},
		Logger: zaptest.NewLogger(t),
	}

	instance.state.Round.Store(specqbft.Round(1))
	instance.state.Identifier.Store(spectypes.NewMsgID([]byte("Lambda"), spectypes.BNRoleAttester))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(specqbft.Round(0))

	consensusMessage := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("Lambda"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
	}

	roundChangeData, err := consensusMessage.GetRoundChangeData()
	require.NoError(t, err)

	t.Run("no change round quorum, not justified", func(t *testing.T) {
		// change round no quorum
		msg := SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], consensusMessage)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		msg = SignMsg(t, operatorIds[1:2], secretKeys[operatorIds[1]], consensusMessage)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		// no quorum achieved, can't justify
		err := instance.JustifyPrePrepare(2, nil)
		require.EqualError(t, err, "no change round quorum")
	})

	t.Run("change round quorum, justified", func(t *testing.T) {
		// test justified change round
		msg := SignMsg(t, operatorIds[2:3], secretKeys[operatorIds[2]], consensusMessage)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		// quorum achieved, can justify
		err := instance.JustifyPrePrepare(2, nil)
		require.NoError(t, err)
	})

	t.Run("any value can be in pre-prepare", func(t *testing.T) {
		require.NoError(t, instance.JustifyPrePrepare(2, []byte("wrong value")))
	})
}

func TestUponPrePrepareHappyFlow(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)
	identifier := spectypes.NewMsgID([]byte("Lambda"), spectypes.BNRoleAttester)
	share := &beacon.Share{
		Committee:   nodes,
		NodeID:      operatorIds[0],
		PublicKey:   secretKeys[operatorIds[0]].GetPublicKey(),
		OperatorIds: shareOperatorIds,
	}
	state := &qbft.State{}
	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.ProposalMsgType: inmem.New(3, 2),
			specqbft.PrepareMsgType:  inmem.New(3, 2),
		},
		Config:         qbft.DefaultConsensusParams(),
		state:          state,
		ValidatorShare: share,
		Logger:         zaptest.NewLogger(t),
		network:        network,
		LeaderSelector: roundrobin.New(share, state),
		signer:         newTestSigner(),
	}

	instance.state.Round.Store(specqbft.Round(1))
	instance.state.Identifier.Store(identifier)
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(specqbft.Round(0))
	instance.state.Height.Store(specqbft.Height(0))
	instance.state.Stage.Store(int32(qbft.RoundStateNotStarted))

	instance.fork = testingFork(instance)

	// test happy flow
	msg := SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Round:      1,
		Identifier: identifier[:],
		Data:       proposalDataToBytes(t, &specqbft.ProposalData{Data: []byte(time.Now().Weekday().String())}),
	})
	require.NoError(t, instance.PrePrepareMsgPipeline().Run(msg))
	msgs := instance.containersMap[specqbft.ProposalMsgType].ReadOnlyMessagesByRound(1)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0])
	require.True(t, instance.State().Stage.Load() == int32(qbft.RoundStatePrePrepare))

	// return nil if another pre-prepare received.
	require.NoError(t, instance.UponPrePrepareMsg().Run(msg))
}

func TestInstance_JustifyPrePrepare(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.RoundChangeMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			OperatorIds: shareOperatorIds,
		},
		state:   &qbft.State{},
		network: network,
	}

	instance.state.Round.Store(specqbft.Round(1))
	instance.state.PreparedValue.Store([]byte(nil))
	instance.state.PreparedRound.Store(specqbft.Round(0))

	require.NoError(t, instance.JustifyPrePrepare(1, nil))

	// try to justify round 2 without round change
	instance.State().Round.Store(specqbft.Round(2))
	err = instance.JustifyPrePrepare(2, nil)
	require.EqualError(t, err, "no change round quorum")

	// test no change round quorum
	msg := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("lambdas"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
	}
	roundChangeData, err := msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], msg), roundChangeData.PreparedValue)

	msg = &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("lambdas"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
	}
	roundChangeData, err = msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[1:2], secretKeys[operatorIds[1]], msg), roundChangeData.PreparedValue)

	err = instance.JustifyPrePrepare(2, nil)
	require.EqualError(t, err, "no change round quorum")

	// test with quorum of change round
	msg = &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("lambdas"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
	}
	roundChangeData, err = msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[2:3], secretKeys[operatorIds[2]], msg), roundChangeData.PreparedValue)

	err = instance.JustifyPrePrepare(2, nil)
	require.NoError(t, err)
}

func TestPrePreparePipeline(t *testing.T) {
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			PublicKey:   sks[operatorIds[0]].GetPublicKey(),
			OperatorIds: shareOperatorIds,
		},
		state: &qbft.State{},
		LeaderSelector: &constant.Constant{
			LeaderIndex: 0,
			OperatorIDs: shareOperatorIds,
		},
	}

	instance.state.Round.Store(specqbft.Round(1))
	instance.state.Identifier.Store(spectypes.NewMsgID([]byte("Lambda"), spectypes.BNRoleAttester))
	instance.state.Height.Store(specqbft.Height(0))

	instance.fork = testingFork(instance)

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

func (s *testSigner) SignIBFTMessage(data message.Root, pk []byte, sigType message.SignatureType) ([]byte, error) {
	return nil, nil
}

func (s *testSigner) SignAttestation(data *spec.AttestationData, duty *spectypes.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}

func proposalDataToBytes(t *testing.T, input *specqbft.ProposalData) []byte {
	ret, err := json.Marshal(input)
	require.NoError(t, err)
	return ret
}
