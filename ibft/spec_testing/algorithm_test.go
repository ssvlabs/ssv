package spec_testing

import (
	"testing"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/proto"
)

// IBFT ALGORITHM 2: Happy flow - a normal case operation
func TestHappyFlow(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	instance := prepareInstance(t, nodes, secretKeys)

	//// UPON receiving valid PRE-PREPARE message
	require.NoError(t, instance.UponPrePrepareMsg().Run(setupMessage(2, secretKeys[2], proto.RoundState_PrePrepare, 1)))

	// ...such that JUSTIFY PREPARE is true
	res, err := instance.JustifyPrePrepare(1)
	require.NoError(t, err)
	require.True(t, res)

	// broadcasts PREPARE messages - 1, 2, 3
	instance.PrepareMessages.AddMessage(setupMessage(0, secretKeys[0], proto.RoundState_Prepare, 1))
	instance.PrepareMessages.AddMessage(setupMessage(1, secretKeys[1], proto.RoundState_Prepare, 1))
	instance.PrepareMessages.AddMessage(setupMessage(2, secretKeys[2], proto.RoundState_Prepare, 1))

	// UPON receiving a quorum of valid PREPARE messages
	res, totalSignedMsgs, committeeSize := instance.PrepareQuorum(1, []byte(time.Now().Weekday().String()))
	require.True(t, res)
	require.EqualValues(t, totalSignedMsgs, 3)
	require.EqualValues(t, committeeSize, 4)

	// broadcasts COMMIT messages - 1, 2, 3
	//instance.CommitMessages.AddMessage(setupMessage(0, secretKeys[0], proto.RoundState_Commit, 1))
	//instance.CommitMessages.AddMessage(setupMessage(1, secretKeys[1], proto.RoundState_Commit, 1))
	//instance.CommitMessages.AddMessage(setupMessage(2, secretKeys[2], proto.RoundState_Commit, 1))

	// UPON receiving a quorum of valid COMMIT messages
	res, totalSignedMsgs, committeeSize = instance.PrepareQuorum(1, []byte(time.Now().Weekday().String()))
	require.True(t, res)
	require.EqualValues(t, totalSignedMsgs, 3)
	require.EqualValues(t, committeeSize, 4)

	//TODO: add DECIDE(λi, value,Qcommit)
}

func TestFailedQuorumWithDuplicatedNodeSignatures(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	instance := prepareInstance(t, nodes, secretKeys)

	//// UPON receiving valid PRE-PREPARE message
	require.NoError(t, instance.UponPrePrepareMsg().Run(setupMessage(2, secretKeys[2], proto.RoundState_PrePrepare, 1)))

	// ...such that JUSTIFY PREPARE is true
	res, err := instance.JustifyPrePrepare(1)
	require.NoError(t, err)
	require.True(t, res)

	// broadcasts PREPARE messages - 1, 2, 3
	instance.PrepareMessages.AddMessage(setupMessage(0, secretKeys[0], proto.RoundState_Prepare, 1))
	instance.PrepareMessages.AddMessage(setupMessage(1, secretKeys[0], proto.RoundState_Prepare, 1)) //duplicated keys
	instance.PrepareMessages.AddMessage(setupMessage(2, secretKeys[1], proto.RoundState_Prepare, 1))
	instance.PrepareMessages.AddMessage(setupMessage(2, secretKeys[2], proto.RoundState_Prepare, 1))

	// UPON receiving a quorum of valid PREPARE messages
	res, totalSignedMsgs, committeeSize := instance.PrepareQuorum(1, []byte(time.Now().Weekday().String()))
	require.False(t, res) //should FAIL due to duplicated secretKeys signature
	require.EqualValues(t, totalSignedMsgs, 4)
	require.EqualValues(t, committeeSize, 4)
}

func setupMessage(id uint64, secretKey *bls.SecretKey, roundState proto.RoundState, round uint64) *proto.SignedMessage {
	return SignMsg(id, secretKey, &proto.Message{
		Type:   roundState,
		Round:  round,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
}
