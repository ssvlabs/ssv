package eventdatahandler

import (
	"context"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bloxapp/ssv/eth/eventbatcher"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func TestExecuteTask(t *testing.T) {
	logger, observedLogs := setupLogsCapture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	edh, err := setupDataHandler(t, ctx, logger)
	require.NoError(t, err)

	t.Run("test AddValidator task execution - not started", func(t *testing.T) {
		logValidatorAdded := unmarshalLog(t, rawValidatorAdded)
		validatorAddedEvent, err := edh.eventParser.ParseValidatorAdded(logValidatorAdded)
		if err != nil {
			t.Fatal("parse ValidatorAdded", err)
		}
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: validatorAddedEvent.PublicKey,
			},
		}
		task := NewStartValidatorTask(edh.taskExecutor, share)
		require.NoError(t, task.Execute())
		require.NotZero(t, observedLogs.Len())
		entry := observedLogs.All()[len(observedLogs.All())-1]
		require.Equal(t, "validator wasn't started", entry.Message)
	})

	t.Run("test AddValidator task execution - started", func(t *testing.T) {
		logValidatorAdded := unmarshalLog(t, rawValidatorAdded)
		validatorAddedEvent, err := edh.eventParser.ParseValidatorAdded(logValidatorAdded)
		if err != nil {
			t.Fatal("parse ValidatorAdded", err)
		}
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: validatorAddedEvent.PublicKey,
			},
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Index: 1,
				},
			},
		}
		task := NewStartValidatorTask(edh.taskExecutor, share)
		require.NoError(t, task.Execute())
		require.NotZero(t, observedLogs.Len())
		entry := observedLogs.All()[len(observedLogs.All())-1]
		require.Equal(t, "started validator", entry.Message)
	})

	t.Run("test StopValidator task execution", func(t *testing.T) {
		require.NoError(t, err)
		task := NewStopValidatorTask(edh.taskExecutor, ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"))
		require.NoError(t, task.Execute())
		require.NotZero(t, observedLogs.Len())
		entry := observedLogs.All()[len(observedLogs.All())-1]
		require.Equal(t, "removed validator", entry.Message)
	})

	t.Run("test LiquidateCluster task execution", func(t *testing.T) {
		var shares []*ssvtypes.SSVShare
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"),
			},
		}
		shares = append(shares, share)
		task := NewLiquidateClusterTask(edh.taskExecutor, ethcommon.HexToAddress("0x1"), []uint64{1, 2, 3}, shares)
		require.NoError(t, task.Execute())
		require.NotZero(t, observedLogs.Len())
		entry := observedLogs.All()[len(observedLogs.All())-1]
		require.Equal(t, "removed share", entry.Message)
	})
	t.Run("test ReactivateCluster task execution", func(t *testing.T) {
		var shares []*ssvtypes.SSVShare
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"),
			},
		}
		shares = append(shares, share)
		task := NewReactivateClusterTask(edh.taskExecutor, ethcommon.HexToAddress("0x1"), []uint64{1, 2, 3}, shares)
		require.NoError(t, task.Execute())
		require.NotZero(t, observedLogs.Len())
		entry := observedLogs.All()[len(observedLogs.All())-1]
		require.Equal(t, "started share", entry.Message)
	})
	t.Run("test UpdateFeeRecipient task execution", func(t *testing.T) {
		task := NewUpdateFeeRecipientTask(edh.taskExecutor, ethcommon.HexToAddress("0x1"), ethcommon.HexToAddress("0x2"))
		require.NoError(t, task.Execute())
		require.NotZero(t, observedLogs.Len())
		entry := observedLogs.All()[len(observedLogs.All())-1]
		require.Equal(t, "started share", entry.Message) // no new logs
	})
}

func TestHandleBlockEventsStreamWithExecution(t *testing.T) {
	logger, observedLogs := setupLogsCapture()
	eb := eventbatcher.NewEventBatcher()
	logValidatorAdded := unmarshalLog(t, rawValidatorAdded)
	events := []ethtypes.Log{
		logValidatorAdded,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	edh, err := setupDataHandler(t, ctx, logger)
	if err != nil {
		t.Fatal(err)
	}
	eventsCh := make(chan ethtypes.Log)
	go func() {
		defer close(eventsCh)
		for _, event := range events {
			eventsCh <- event
		}
	}()
	lastProcessedBlock, err := edh.HandleBlockEventsStream(eb.BatchEvents(eventsCh), true)
	require.Equal(t, uint64(0x89EBFF), lastProcessedBlock)
	require.NoError(t, err)
	var observedLogsFlow []string
	for _, entry := range observedLogs.All() {
		observedLogsFlow = append(observedLogsFlow, entry.Message)
	}
	happyFlow := []string{
		"setup operator key is DONE!",
		"CreatingController",
		"processing events from block",
		"processing event",
		"malformed event: failed to verify signature",
	}
	require.Equal(t, happyFlow, observedLogsFlow)
}

func setupLogsCapture() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	return zap.New(core), logs
}
