package duties

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

func TestVoluntaryExitHandler_HandleGenesisDuties(t *testing.T) {
	exitCh := make(chan ExitDescriptor)
	handler := NewVoluntaryExitHandler(dutystore.NewVoluntaryExit(), exitCh)

	currentSlot := &SafeValue[phase0.Slot]{}
	currentSlot.Set(0)

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, goclient.FarFutureEpoch)
	startFn()

	blockByNumberCalls := create1to1BlockSlotMapping(scheduler)
	assert1to1BlockSlotMapping(t, scheduler)
	require.EqualValues(t, 1, blockByNumberCalls.Load())

	executeDutiesCall := make(chan []*genesisspectypes.Duty)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, 1)

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

	expectedDuties := expectedExecutedVoluntaryExitGenesisDuties(allDescriptors)

	require.EqualValues(t, 1, blockByNumberCalls.Load())
	exitCh <- normalExit

	t.Run("slot = 0, block = 1 - no execution", func(t *testing.T) {
		currentSlot.Set(0)
		ticker.Send(currentSlot.Get())
		waitForNoActionGenesis(t, logger, nil, executeDutiesCall, timeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 1, block = 1 - no execution", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(normalExit.BlockNumber))
		ticker.Send(currentSlot.Get())
		waitForNoActionGenesis(t, logger, nil, executeDutiesCall, timeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 4, block = 1 - no execution", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone - 1)
		ticker.Send(currentSlot.Get())
		waitForNoActionGenesis(t, logger, nil, executeDutiesCall, timeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 5, block = 1 - executing duty, fetching block number", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		ticker.Send(currentSlot.Get())
		waitForGenesisDutiesExecution(t, logger, nil, executeDutiesCall, timeout, expectedDuties[:1])
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	exitCh <- sameBlockExit

	t.Run("slot = 5, block = 1 - executing another duty, no block number fetch", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(sameBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		ticker.Send(currentSlot.Get())
		waitForGenesisDutiesExecution(t, logger, nil, executeDutiesCall, timeout, expectedDuties[1:2])
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	exitCh <- newBlockExit

	t.Run("slot = 5, block = 2 - no execution", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(normalExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		ticker.Send(currentSlot.Get())
		waitForNoActionGenesis(t, logger, nil, executeDutiesCall, timeout)
		require.EqualValues(t, 3, blockByNumberCalls.Load())
	})

	t.Run("slot = 6, block = 1 - executing new duty, fetching block number", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(newBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone)
		ticker.Send(currentSlot.Get())
		waitForGenesisDutiesExecution(t, logger, nil, executeDutiesCall, timeout, expectedDuties[2:3])
		require.EqualValues(t, 3, blockByNumberCalls.Load())
	})

	exitCh <- pastBlockExit

	t.Run("slot = 10, block = 5 - executing past duty, fetching block number", func(t *testing.T) {
		currentSlot.Set(phase0.Slot(pastBlockExit.BlockNumber) + voluntaryExitSlotsToPostpone + 1)
		ticker.Send(currentSlot.Get())
		waitForGenesisDutiesExecution(t, logger, nil, executeDutiesCall, timeout, expectedDuties[3:4])
		require.EqualValues(t, 4, blockByNumberCalls.Load())
	})

	cancel()
	close(exitCh)
	require.NoError(t, schedulerPool.Wait())
}

func expectedExecutedVoluntaryExitGenesisDuties(descriptors []ExitDescriptor) []*genesisspectypes.Duty {
	expectedDuties := make([]*genesisspectypes.Duty, 0)
	for _, d := range descriptors {
		expectedDuties = append(expectedDuties, &genesisspectypes.Duty{
			Type:           genesisspectypes.BNRoleVoluntaryExit,
			PubKey:         d.PubKey,
			Slot:           phase0.Slot(d.BlockNumber) + voluntaryExitSlotsToPostpone,
			ValidatorIndex: d.ValidatorIndex,
		})
	}
	return expectedDuties
}
