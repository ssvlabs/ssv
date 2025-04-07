package duties

import (
	"context"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	mocknetwork "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/mocks"
)

func TestVoluntaryExitHandler_HandleDuties(t *testing.T) {
	exitCh := make(chan ExitDescriptor)
	handler := NewVoluntaryExitHandler(dutystore.NewVoluntaryExit(), exitCh)

	currentSlot := &SafeValue[phase0.Slot]{}
	currentSlot.Set(0)

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot)
	startFn()

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
		currentSlot.Set(0)
		ticker.Send(currentSlot.Get())
		waitForNoAction(t, logger, nil, executeDutiesCall, timeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 1, block = 1 - no execution", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(normalExit.BlockNumber))
		ticker.Send(currentSlot.Get())
		waitForNoAction(t, logger, nil, executeDutiesCall, timeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 4, block = 1 - no execution", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone - 1)
		ticker.Send(currentSlot.Get())
		waitForNoAction(t, logger, nil, executeDutiesCall, timeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 5, block = 1 - executing duty, fetching block number", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		ticker.Send(currentSlot.Get())
		waitForDutiesExecution(t, logger, nil, executeDutiesCall, timeout, expectedDuties[:1])
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	exitCh <- sameBlockExit

	t.Run("slot = 5, block = 1 - executing another duty, no block number fetch", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(sameBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		ticker.Send(currentSlot.Get())
		waitForDutiesExecution(t, logger, nil, executeDutiesCall, timeout, expectedDuties[1:2])
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	exitCh <- newBlockExit

	t.Run("slot = 5, block = 2 - no execution", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		ticker.Send(currentSlot.Get())
		waitForNoAction(t, logger, nil, executeDutiesCall, timeout)
		require.EqualValues(t, 3, blockByNumberCalls.Load())
	})

	t.Run("slot = 6, block = 1 - executing new duty, fetching block number", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(newBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		ticker.Send(currentSlot.Get())
		waitForDutiesExecution(t, logger, nil, executeDutiesCall, timeout, expectedDuties[2:3])
		require.EqualValues(t, 3, blockByNumberCalls.Load())
	})

	exitCh <- pastBlockExit

	t.Run("slot = 10, block = 5 - executing past duty, fetching block number", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(pastBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone + 1)
		ticker.Send(currentSlot.Get())
		waitForDutiesExecution(t, logger, nil, executeDutiesCall, timeout, expectedDuties[3:4])
		require.EqualValues(t, 4, blockByNumberCalls.Load())
	})

	cancel()
	close(exitCh)
	require.NoError(t, schedulerPool.Wait())
}

func create1to1BlockSlotMapping(scheduler *Scheduler) *atomic.Uint64 {
	var blockByNumberCalls atomic.Uint64

	scheduler.executionClient.(*MockExecutionClient).EXPECT().BlockByNumber(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error) {
			blockByNumberCalls.Add(1)
			expectedBlock := ethtypes.NewBlock(&ethtypes.Header{Time: blockNumber.Uint64()}, nil, nil, trie.NewStackTrie(nil))
			return expectedBlock, nil
		},
	).AnyTimes()
	scheduler.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().EstimatedSlotAtTime(gomock.Any()).DoAndReturn(
		func(time int64) phase0.Slot {
			return phase0.Slot(time)
		},
	).AnyTimes()

	return &blockByNumberCalls
}

func assert1to1BlockSlotMapping(t *testing.T, scheduler *Scheduler) {
	const blockNumber = 123

	block, err := scheduler.executionClient.BlockByNumber(context.TODO(), new(big.Int).SetUint64(blockNumber))
	require.NoError(t, err)
	require.NotNil(t, block)

	slot := scheduler.network.Beacon.EstimatedSlotAtTime(int64(block.Time()))
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
