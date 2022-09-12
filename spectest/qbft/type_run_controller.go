package qbft

import (
	"bytes"
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv-spec/qbft"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protcolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"reflect"
	"testing"
	"time"
)

// RunControllerSpecTest runs spec test type ControllerSpecTest
func RunControllerSpecTest(t *testing.T, test *spectests.ControllerSpecTest) {
	ctx := context.TODO()
	defer ctx.Done()
	logger := logex.Build(test.Name, zapcore.DebugLevel, nil)

	identifier := spectypes.NewMsgID(testingutils.TestingValidatorPubKey[:], spectypes.BNRoleAttester)
	db, qbftStorage := NewQBFTStorage(ctx, t, logger, spectypes.BNRoleAttester.String())
	defer func() {
		db.Close()
	}()
	share, keySet := ToMappedShare(t, testingutils.TestingShare(testingutils.Testing4SharesSet()))
	pi, _ := protcolp2p.GenPeerID()
	p2pNet := protcolp2p.NewMockNetwork(logger, pi, 10)
	beacon := validator.NewTestBeacon(t)
	forkVersion := forksprotocol.GenesisForkVersion
	ctrl := NewController(ctx, t, logger, identifier, qbftStorage, share, p2pNet, beacon, forkVersion)
	require.NoError(t, ctrl.Init())

	p2pNet.(BroadcastMessagesGetter).SetCalledDecidedSyncCnt(0) // remove previews init sync calls. need this as long as spec not supporting initial sync

	// add share key to account
	require.NoError(t, ctrl.KeyManager.AddShare(keySet.Shares[share.NodeID]))

	storedInstances := make([]instance.Instancer, 0)

	var lastErr error
	for _, runData := range test.RunInstanceData {
		var decidedCnt = uint(0)

		getInstance := func(instance instance.Instancer) {
			storedInstances = append(storedInstances, instance)
			if runData.DecidedCnt == 0 {
				time.Sleep(time.Second * 1) // wait for instance to warm up
				instance.Stop()             // in order to continue with test
			}
		}

		res, err := ctrl.StartInstance(instance.ControllerStartInstanceOptions{
			Logger:          logger,
			Height:          0,
			Value:           runData.InputValue,
			RequireMinPeers: false,
		}, getInstance)
		if err != nil {
			lastErr = err
			if lastDecided, err := ctrl.DecidedStrategy.GetLastDecided(ctrl.GetIdentifier()); err == nil {
				if lastDecided != nil { // mock case when future decided close current instance but for different height. (temp until aligned with spec logic)
					decidedCnt++
				}
			}
		} else {
			if res.Decided {
				decidedCnt++
			}
		}

		for _, msg := range runData.InputMessages {
			encoded, err := msg.Encode()
			require.NoError(t, err)
			ssvMsg := &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   message.ToMessageID(msg.Message.Identifier),
				Data:    encoded,
			}
			err = ctrl.ProcessMsg(ssvMsg)
			if err != nil {
				lastErr = err
			}
		}

		require.EqualValues(t, runData.DecidedCnt, decidedCnt)

		if runData.SavedDecided != nil {
			decided, err := ctrl.DecidedStrategy.GetLastDecided(ctrl.GetIdentifier())
			require.NoError(t, err)
			require.NotNil(t, decided)
			r1, err := decided.GetRoot()
			require.NoError(t, err)
			r2, err := runData.SavedDecided.GetRoot()
			require.NoError(t, err)

			require.EqualValues(t, r2, r1)
			require.EqualValues(t, runData.SavedDecided.Signers, decided.Signers)
			require.EqualValues(t, runData.SavedDecided.Signature, decided.Signature)

			broadcastMsgs := p2pNet.(BroadcastMessagesGetter).GetBroadcastMessages()
			require.Greater(t, len(broadcastMsgs), 0)

			found := false
			for _, msg := range broadcastMsgs {
				if msg.MsgType == spectypes.SSVDecidedMsgType && bytes.Equal(identifier[:], msg.MsgID[:]) {
					msg2 := &qbft.SignedMessage{}
					require.NoError(t, msg2.Decode(msg.Data))
					r1, err := msg2.GetRoot()
					require.NoError(t, err)

					if bytes.Equal(r1, r2) &&
						reflect.DeepEqual(runData.SavedDecided.Signers, msg2.Signers) &&
						reflect.DeepEqual(runData.SavedDecided.Signature, msg2.Signature) {
						require.False(t, found)
						found = true
					}
				}
			}
			require.True(t, found)
		}

		decidedSyncCalledCnt := p2pNet.(BroadcastMessagesGetter).CalledDecidedSyncCnt()
		require.EqualValues(t, runData.DecidedSyncCallCnt, decidedSyncCalledCnt)

		if len(runData.ControllerPostRoot) > 0 {
			r, err := GetControllerRoot(t, ctrl, storedInstances)
			require.NoError(t, err)
			require.EqualValues(t, runData.ControllerPostRoot, hex.EncodeToString(r))
		}
	}
	ErrorHandling(t, test.ExpectedError, lastErr)
}
