package ibft

import (
	"testing"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
)

// IBFT ALGORITHM 2: Happy flow - a normal case operation
func TestUponPrePrepareMessagesBroadcastsPrepare(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := setupInstance(t, nodes, secretKeys)

	// Upon receiving valid PRE-PREPARE messages - 1, 2, 3
	firstMessage := setupMessage(1, secretKeys[1], proto.RoundState_PrePrepare)
	instance.prePrepareMessages.AddMessage(firstMessage)

	secondMessage := setupMessage(1, secretKeys[2], proto.RoundState_PrePrepare)
	instance.prePrepareMessages.AddMessage(secondMessage)

	thirdMessage := setupMessage(1, secretKeys[3], proto.RoundState_PrePrepare)
	instance.prePrepareMessages.AddMessage(thirdMessage)

	require.NoError(t, instance.uponPrePrepareMsg().Run(thirdMessage))

	// ...such that JUSTIFY PREPARE is true
	runJustifyPrePrepare(t, instance, 1)

	// broadcasts PREPARE message
	prepareMessage := setupMessage(1, secretKeys[1], proto.RoundState_Prepare)
	instance.prepareMessages.AddMessage(prepareMessage)
}

func setupMessage(id uint64, secretKeys *bls.SecretKey, roundState proto.RoundState) *proto.SignedMessage {
	return signMsg(id, secretKeys, &proto.Message{
		Type:   roundState,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
}

func runJustifyPrePrepare(t *testing.T, instance *Instance, round uint64) {
	res, err := instance.JustifyPrePrepare(round)
	require.NoError(t, err)
	require.True(t, res)
}

func setupInstance(t *testing.T, nodes map[uint64]*proto.Node, secretKeys map[uint64]*bls.SecretKey) *Instance {
	return &Instance{
		prePrepareMessages:  msgcontinmem.New(),
		prepareMessages:     msgcontinmem.New(),
		changeRoundMessages: msgcontinmem.New(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			Lambda:        []byte("Lamba"),
			PreparedRound: 0,
			PreparedValue: nil,
		},
		Me: &proto.Node{
			IbftId: 0,
			Pk:     nodes[0].Pk,
			Sk:     secretKeys[0].Serialize(),
		},
		consensus: bytesval.New([]byte(time.Now().Weekday().String())),
		logger:    zaptest.NewLogger(t),
	}
}
