package instance

import (
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/constant"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/roundrobin"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/proposal"
)

func TestJustifyProposalAfterChangeRoundPrepared(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	value, err := (&specqbft.ProposalData{Data: []byte(time.Now().Weekday().String())}).Encode()
	require.NoError(t, err)
	wrongValue, err := (&specqbft.ProposalData{Data: []byte("wrong value")}).Encode()
	require.NoError(t, err)

	instance := &Instance{
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.ProposalMsgType:    inmem.New(3, 2),
			specqbft.RoundChangeMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		State:  &qbft.State{},
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			OperatorIds: shareOperatorIds,
		},
		Logger: zaptest.NewLogger(t),
	}

	identifier := []byte("Identifier")
	messageID := spectypes.NewMsgID(identifier, spectypes.BNRoleAttester)
	instance.GetState().Round.Store(specqbft.Round(1))
	instance.GetState().Identifier.Store(messageID[:])
	instance.GetState().PreparedValue.Store([]byte(nil))
	instance.GetState().PreparedRound.Store(specqbft.Round(0))

	prepareMessage := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Round:      specqbft.FirstRound,
		Identifier: identifier,
		Data: prepareDataToBytes(t, &specqbft.PrepareData{
			Data: value,
		}),
	}

	prepareMessages := []*specqbft.SignedMessage{
		SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], prepareMessage),
		SignMsg(t, operatorIds[1:2], secretKeys[operatorIds[1]], prepareMessage),
		SignMsg(t, operatorIds[2:3], secretKeys[operatorIds[2]], prepareMessage),
	}

	roundChangeMessage := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: identifier,
		Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
			PreparedRound:            1,
			PreparedValue:            value,
			RoundChangeJustification: prepareMessages,
		}),
	}

	roundChangeData, err := roundChangeMessage.GetRoundChangeData()
	require.NoError(t, err)

	t.Run("not quorum, not justified", func(t *testing.T) {
		// change round no quorum
		msg := SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], roundChangeMessage)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		// no quorum achieved, err
		msg = SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Round:      2,
			Identifier: identifier,
			Data:       value,
		})
		instance.ContainersMap[specqbft.ProposalMsgType].AddMessage(msg, roundChangeData.PreparedValue)
		err := proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, nil, nil, value)
		require.EqualError(t, err, "change round has not quorum")
	})

	t.Run("change round quorum, justified", func(t *testing.T) {
		// test justified change round
		msg := SignMsg(t, operatorIds[1:2], secretKeys[operatorIds[1]], roundChangeMessage)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)
		msg = SignMsg(t, operatorIds[2:3], secretKeys[operatorIds[2]], roundChangeMessage)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		roundChanges := instance.ContainersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(2)
		err := proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, roundChanges, prepareMessages, value)
		require.NoError(t, err)
	})

	t.Run("wrong value, unjustified", func(t *testing.T) {
		roundChanges := instance.ContainersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(2)
		err := proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, roundChanges, prepareMessages, wrongValue)
		require.EqualError(t, err, "proposed data doesn't match highest prepared")
	})
}

func TestJustifyProposalAfterChangeRoundNoPrepare(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	instance := &Instance{
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.ProposalMsgType:    inmem.New(3, 2),
			specqbft.PrepareMsgType:     inmem.New(3, 2),
			specqbft.RoundChangeMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		State:  &qbft.State{},
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			OperatorIds: shareOperatorIds,
		},
		Logger: zaptest.NewLogger(t),
	}

	messageID := spectypes.NewMsgID([]byte("Identifier"), spectypes.BNRoleAttester)
	instance.GetState().Round.Store(specqbft.Round(1))
	instance.GetState().Identifier.Store(messageID[:])
	instance.GetState().PreparedValue.Store([]byte(nil))
	instance.GetState().PreparedRound.Store(specqbft.Round(0))

	consensusMessage := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("Identifier"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
	}

	roundChangeData, err := consensusMessage.GetRoundChangeData()
	require.NoError(t, err)

	t.Run("no change round quorum, not justified", func(t *testing.T) {
		// change round no quorum
		msg := SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], consensusMessage)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		msg = SignMsg(t, operatorIds[1:2], secretKeys[operatorIds[1]], consensusMessage)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		// no quorum achieved, can't justify
		err := proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, nil, nil, nil)
		require.EqualError(t, err, "change round has not quorum")
	})

	t.Run("change round quorum, justified", func(t *testing.T) {
		// test justified change round
		msg := SignMsg(t, operatorIds[2:3], secretKeys[operatorIds[2]], consensusMessage)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg, roundChangeData.PreparedValue)

		// quorum achieved, can justify
		roundChanges := instance.ContainersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(2)
		err := proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, roundChanges, nil, nil)
		require.NoError(t, err)
	})

	t.Run("any value can be in proposal", func(t *testing.T) {
		roundChanges := instance.ContainersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(2)
		require.NoError(t, proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, roundChanges, nil, []byte("wrong value")))
	})
}

func TestUponProposalHappyFlow(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)
	identifier := spectypes.NewMsgID([]byte("Identifier"), spectypes.BNRoleAttester)
	share := &beacon.Share{
		Committee:   nodes,
		NodeID:      operatorIds[0],
		PublicKey:   secretKeys[operatorIds[0]].GetPublicKey(),
		OperatorIds: shareOperatorIds,
	}
	state := &qbft.State{}
	instance := &Instance{
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.ProposalMsgType: inmem.New(3, 2),
			specqbft.PrepareMsgType:  inmem.New(3, 2),
		},
		Config:         qbft.DefaultConsensusParams(),
		State:          state,
		ValidatorShare: share,
		Logger:         zaptest.NewLogger(t),
		network:        network,
		LeaderSelector: roundrobin.New(share, state),
		SsvSigner:      newTestSSVSigner(),
	}

	instance.GetState().Round.Store(specqbft.Round(1))
	instance.GetState().Identifier.Store(identifier[:])
	instance.GetState().PreparedValue.Store([]byte(nil))
	instance.GetState().PreparedRound.Store(specqbft.Round(0))
	instance.GetState().Height.Store(specqbft.Height(0))
	instance.GetState().Stage.Store(int32(qbft.RoundStateNotStarted))

	instance.fork = testingFork(instance)

	// test happy flow
	msg := SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Round:      1,
		Identifier: identifier[:],
		Data:       proposalDataToBytes(t, &specqbft.ProposalData{Data: []byte(time.Now().Weekday().String())}),
	})
	require.NoError(t, instance.ProposalMsgPipeline().Run(msg))
	msgs := instance.ContainersMap[specqbft.ProposalMsgType].ReadOnlyMessagesByRound(1)
	require.Len(t, msgs, 1)
	require.NotNil(t, msgs[0])
	require.True(t, instance.GetState().Stage.Load() == int32(qbft.RoundStateProposal))

	// return nil if another proposal received.
	require.NoError(t, instance.UponProposalMsg().Run(msg))
}

func TestInstance_JustifyProposal(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	network := protocolp2p.NewMockNetwork(zap.L(), pi, 10)

	instance := &Instance{
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.RoundChangeMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			OperatorIds: shareOperatorIds,
		},
		State:   &qbft.State{},
		network: network,
	}

	instance.GetState().Round.Store(specqbft.Round(1))
	instance.GetState().PreparedValue.Store([]byte(nil))
	instance.GetState().PreparedRound.Store(specqbft.Round(0))

	require.NoError(t, proposal.Justify(instance.ValidatorShare, instance.GetState(), 1, nil, nil, nil))

	// try to justify round 2 without round change
	instance.GetState().Round.Store(specqbft.Round(2))
	err = proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, nil, nil, nil)
	require.EqualError(t, err, "change round has not quorum")

	// test no change round quorum
	msg := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("identifiers"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
	}
	roundChangeData, err := msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[:1], secretKeys[operatorIds[0]], msg), roundChangeData.PreparedValue)

	msg = &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("identifiers"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
	}
	roundChangeData, err = msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[1:2], secretKeys[operatorIds[1]], msg), roundChangeData.PreparedValue)

	err = proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, nil, nil, nil)
	require.EqualError(t, err, "change round has not quorum")

	// test with quorum of change round
	msg = &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Round:      2,
		Identifier: []byte("identifiers"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
	}
	roundChangeData, err = msg.GetRoundChangeData()
	require.NoError(t, err)
	instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[2:3], secretKeys[operatorIds[2]], msg), roundChangeData.PreparedValue)

	roundChanges := instance.ContainersMap[specqbft.RoundChangeMsgType].ReadOnlyMessagesByRound(2)
	err = proposal.Justify(instance.ValidatorShare, instance.GetState(), 2, roundChanges, nil, nil)
	require.NoError(t, err)
}

func TestProposalPipeline(t *testing.T) {
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	instance := &Instance{
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			PublicKey:   sks[operatorIds[0]].GetPublicKey(),
			OperatorIds: shareOperatorIds,
		},
		State: &qbft.State{},
		LeaderSelector: &constant.Constant{
			LeaderIndex: 0,
			OperatorIDs: shareOperatorIds,
		},
	}

	instance.GetState().Round.Store(specqbft.Round(1))
	messageID := spectypes.NewMsgID([]byte("Identifier"), spectypes.BNRoleAttester)
	instance.GetState().Identifier.Store(messageID[:])
	instance.GetState().Height.Store(specqbft.Height(0))

	instance.fork = testingFork(instance)

	pipeline := instance.ProposalMsgPipeline()
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, sequence, identifier, authorize, validate proposal, , add proposal msg, upon proposal msg, ", pipeline.Name())
}

type testSSVSigner struct {
}

func newTestSSVSigner() spectypes.SSVSigner {
	return &testSSVSigner{}
}

func (s *testSSVSigner) Decrypt(pk *rsa.PublicKey, cipher []byte) ([]byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) Encrypt(pk *rsa.PublicKey, data []byte) ([]byte, error) {
	panic("implement me")
}

func (s *testSSVSigner) SignRoot(data spectypes.Root, sigType spectypes.SignatureType, pk []byte) (spectypes.Signature, error) {
	return nil, nil
}

func proposalDataToBytes(t *testing.T, input *specqbft.ProposalData) []byte {
	ret, err := json.Marshal(input)
	require.NoError(t, err)
	return ret
}
