package qbft

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/utils/logex"
)

// RunMsgProcessingSpecTest for spec test type MsgProcessingSpecTest
func RunMsgProcessingSpecTest(t *testing.T, test *spectests.MsgProcessingSpecTest) {
	ctx := context.TODO()
	defer ctx.Done()
	logger := logex.Build(test.Name, zapcore.DebugLevel, nil)

	identifier := test.Pre.State.ID
	forkVersion := forksprotocol.GenesisForkVersion
	pi, _ := protocolp2p.GenPeerID()
	p2pNet := protocolp2p.NewMockNetwork(logger, pi, 10)
	beacon := validator.NewTestBeacon(t)

	db, qbftStorage := NewQBFTStorage(ctx, t, logger, spectypes.BNRoleAttester.String())
	defer func() {
		db.Close()
	}()
	share, keySet := ToMappedShare(t, test.Pre.State.Share)

	qbftInstance := NewQbftInstance(logger, qbftStorage, p2pNet, beacon, share, identifier[:], forkVersion)
	qbftInstance.Init()
	qbftInstance.GetState().InputValue.Store(test.Pre.StartValue)
	qbftInstance.GetState().Round.Store(test.Pre.State.Round)
	qbftInstance.GetState().Height.Store(test.Pre.State.Height)
	qbftInstance.GetState().ProposalAcceptedForCurrentRound.Store(test.Pre.State.ProposalAcceptedForCurrentRound)

	// add share key to account
	require.NoError(t, beacon.KeyManager.AddShare(keySet.Shares[share.NodeID]))

	var lastErr error
	for _, msg := range test.InputMessages {
		if _, err := qbftInstance.ProcessMsg(msg); err != nil {
			lastErr = err
		}
	}

	mappedInstance := MapToSpecInstance(t, identifier, qbftInstance, share)

	ErrorHandling(t, test.ExpectedError, lastErr)

	encode, _ := mappedInstance.State.Encode()
	fmt.Println("instance root -")
	fmt.Println(string(encode)) // TODO REMOVE AFTER
	mappedRoot, err := mappedInstance.State.GetRoot()
	require.NoError(t, err)
	require.Equal(t, test.PostRoot, hex.EncodeToString(mappedRoot))

	outputMessages := p2pNet.(BroadcastMessagesGetter).GetBroadcastMessages()
	require.Equal(t, len(test.OutputMessages), len(outputMessages))

	// TODO needed for version v0.3.2. need to revert on version v0.3.3
	/*for i, outputMessage := range outputMessages {
		msg1 := test.OutputMessages[i]
		r1, _ := msg1.GetRoot()

		msg2 := &specqbft.SignedMessage{}
		require.NoError(t, msg2.Decode(outputMessage.Data))

		r2, _ := msg2.GetRoot()
		require.EqualValues(t, r1, r2, fmt.Sprintf("output msg %d roots not equal", i))
	}*/
}
