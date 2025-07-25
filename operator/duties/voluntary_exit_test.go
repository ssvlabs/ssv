package duties

import (
	"context"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

func TestVoluntaryExitHandler_HandleDuties(t *testing.T) {
	t.Parallel()

	exitCh := make(chan ExitDescriptor)
	handler := NewVoluntaryExitHandler(dutystore.NewVoluntaryExit(), exitCh)

	ctx, cancel := context.WithCancel(t.Context())

	// Set genesis time far enough in the past so that small block numbers
	// (used as seconds-since-epoch in test headers) are always after genesis.
	//
	// Ensure genesis is not in the future relative to mocked block timestamps (1,2,5... seconds).
	//
	// Use 1-second slots so that block number == slot in the test’s 1:1 mapping assertion.
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, time.Unix(0, 0), time.Second)

	startScheduler(ctx, t, scheduler, schedulerPool)

	blockByNumberCalls := create1to1BlockSlotMapping(scheduler)
	assert1to1BlockSlotMapping(t, scheduler)
	require.EqualValues(t, 1, blockByNumberCalls.Load())

	executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
	setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

	const blockNumber = uint64(1)

	normalExit := ExitDescriptor{
		OwnValidator:   true,
		PubKey:         phase0.BLSPubKey{1, 2, 3},
		ValidatorIndex: phase0.ValidatorIndex(1),
		BlockNumber:    blockNumber,
	}
	sameBlockExit := ExitDescriptor{
		OwnValidator:   true,
		PubKey:         phase0.BLSPubKey{4, 5, 6},
		ValidatorIndex: phase0.ValidatorIndex(2),
		BlockNumber:    normalExit.BlockNumber,
	}
	newBlockExit := ExitDescriptor{
		OwnValidator:   true,
		PubKey:         phase0.BLSPubKey{1, 2, 3},
		ValidatorIndex: phase0.ValidatorIndex(1),
		BlockNumber:    normalExit.BlockNumber + 1,
	}
	pastBlockExit := ExitDescriptor{
		OwnValidator:   true,
		PubKey:         phase0.BLSPubKey{1, 2, 3},
		ValidatorIndex: phase0.ValidatorIndex(1),
		BlockNumber:    normalExit.BlockNumber + 4,
	}

	allDescriptors := []ExitDescriptor{
		normalExit,
		sameBlockExit,
		newBlockExit,
		pastBlockExit,
	}

	expectedDuties := expectedExecutedVoluntaryExitDuties(allDescriptors)

	require.EqualValues(t, 1, blockByNumberCalls.Load())
	exitCh <- normalExit

	t.Run("slot = 0, block = 1 - no execution", func(t *testing.T) {
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, nil, executeDutiesCall, noActionTimeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 1, block = 1 - no execution", func(t *testing.T) {
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(normalExit.BlockNumber))
		ticker.Send(phase0.Slot(normalExit.BlockNumber))
		waitForNoAction(t, nil, executeDutiesCall, noActionTimeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 4, block = 1 - no execution", func(t *testing.T) {
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(normalExit.BlockNumber)+voluntaryExitSlotsToPostpone-1)
		ticker.Send(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone - 1)
		waitForNoAction(t, nil, executeDutiesCall, noActionTimeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 5, block = 1 - executing duty, fetching block number", func(t *testing.T) {
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(normalExit.BlockNumber)+voluntaryExitSlotsToPostpone)
		ticker.Send(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuties[:1])
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	exitCh <- sameBlockExit

	t.Run("slot = 5, block = 1 - executing another duty, no block number fetch", func(t *testing.T) {
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(sameBlockExit.BlockNumber)+voluntaryExitSlotsToPostpone)
		ticker.Send(phase0.Slot(sameBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuties[1:2])
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	exitCh <- newBlockExit

	t.Run("slot = 5, block = 2 - no execution", func(t *testing.T) {
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(normalExit.BlockNumber)+voluntaryExitSlotsToPostpone)
		ticker.Send(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		waitForNoAction(t, nil, executeDutiesCall, noActionTimeout)
		require.EqualValues(t, 3, blockByNumberCalls.Load())
	})

	t.Run("slot = 6, block = 1 - executing new duty, fetching block number", func(t *testing.T) {
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(newBlockExit.BlockNumber)+voluntaryExitSlotsToPostpone)
		ticker.Send(phase0.Slot(newBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuties[2:3])
		require.EqualValues(t, 3, blockByNumberCalls.Load())
	})

	exitCh <- pastBlockExit

	t.Run("slot = 10, block = 5 - executing past duty, fetching block number", func(t *testing.T) {
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(pastBlockExit.BlockNumber)+voluntaryExitSlotsToPostpone+1)
		ticker.Send(phase0.Slot(pastBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone + 1)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuties[3:4])
		require.EqualValues(t, 4, blockByNumberCalls.Load())
	})

	cancel()
	close(exitCh)
	require.NoError(t, schedulerPool.Wait())
}

func create1to1BlockSlotMapping(scheduler *Scheduler) *atomic.Uint64 {
	var headerByNumberCalls atomic.Uint64

	scheduler.executionClient.(*MockExecutionClient).EXPECT().HeaderByNumber(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
			headerByNumberCalls.Add(1)
			return &ethtypes.Header{Time: blockNumber.Uint64()}, nil
		},
	).AnyTimes()

	return &headerByNumberCalls
}

func assert1to1BlockSlotMapping(t *testing.T, scheduler *Scheduler) {
	const blockNumber = 123

	header, err := scheduler.executionClient.HeaderByNumber(context.TODO(), new(big.Int).SetInt64(blockNumber))
	require.NoError(t, err)
	require.NotNil(t, header)

	slot := scheduler.beaconConfig.EstimatedSlotAtTime(time.Unix(int64(header.Time), 0))
	require.EqualValues(t, blockNumber, slot)
}

func expectedExecutedVoluntaryExitDuties(descriptors []ExitDescriptor) []*spectypes.ValidatorDuty {
	expectedDuties := make([]*spectypes.ValidatorDuty, 0)
	for _, d := range descriptors {
		expectedDuties = append(expectedDuties, &spectypes.ValidatorDuty{
			Type:           spectypes.BNRoleVoluntaryExit,
			PubKey:         d.PubKey,
			Slot:           phase0.Slot(d.BlockNumber) + voluntaryExitSlotsToPostpone,
			ValidatorIndex: d.ValidatorIndex,
		})
	}
	return expectedDuties
}
