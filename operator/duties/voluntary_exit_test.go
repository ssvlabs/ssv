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

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

// TestVoluntaryExitDutySlotPinned guards a wire-format invariant: pre-#2851
// peers compute and validate against blockSlot + 4, so this operator must
// emit the same value for cross-version interop. Bumping the constant is a
// coordinated network-wide upgrade, not a code-cleanup change — this test
// turns any literal change into a visible diff that PR review can catch.
func TestVoluntaryExitDutySlotPinned(t *testing.T) {
	t.Parallel()
	require.EqualValues(t, 4, voluntaryExitDutySlotsToPostpone)
}

func TestVoluntaryExitHandler_HandleDuties(t *testing.T) {
	t.Parallel()

	exitCh := make(chan ExitDescriptor)
	handler := NewVoluntaryExitHandler(dutystore.NewVoluntaryExit(), exitCh)

	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)

	// Set genesis time far enough in the past so that small block numbers
	// (used as seconds-since-epoch in test headers) are always after genesis.
	//
	// Ensure genesis is not in the future relative to mocked block timestamps (1,2,5... seconds).
	//
	// Use 1-second slots so that block number == slot in the test’s 1:1 mapping assertion.
	scheduler, ticker := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, time.Unix(0, 0), time.Second)

	require.NoError(t, scheduler.Start(ctx))

	blockByNumberCalls := create1to1BlockSlotMapping(scheduler)
	assert1to1BlockSlotMapping(t, scheduler)
	require.EqualValues(t, 1, blockByNumberCalls.Load())

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

	t.Run("slot = 0, block = 1 - no execution before block slot", func(t *testing.T) {
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, nil, nil, noActionTimeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = 1, block = 1 - no execution at block slot", func(t *testing.T) {
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(normalExit.BlockNumber))
		ticker.Send(phase0.Slot(normalExit.BlockNumber))
		waitForNoAction(t, nil, nil, noActionTimeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = block + executionPostpone - 1, block = 1 - no execution", func(t *testing.T) {
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(normalExit.BlockNumber)+voluntaryExitExecutionSlotsToPostpone-1)
		ticker.Send(phase0.Slot(normalExit.BlockNumber) + voluntaryExitExecutionSlotsToPostpone - 1)
		waitForNoAction(t, nil, nil, noActionTimeout)
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	t.Run("slot = block + executionPostpone, block = 1 - executing duty, fetching block number", func(t *testing.T) {
		executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
		setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

		ticker.Send(phase0.Slot(normalExit.BlockNumber) + voluntaryExitExecutionSlotsToPostpone)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuties[:1])
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	exitCh <- sameBlockExit

	t.Run("slot = block + executionPostpone, block = 1 - executing another duty, no block number fetch", func(t *testing.T) {
		executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
		setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

		ticker.Send(phase0.Slot(sameBlockExit.BlockNumber) + voluntaryExitExecutionSlotsToPostpone)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuties[1:2])
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	exitCh <- newBlockExit

	t.Run("slot = block + executionPostpone, block = 2 - no execution", func(t *testing.T) {
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(normalExit.BlockNumber)+voluntaryExitExecutionSlotsToPostpone)
		ticker.Send(phase0.Slot(normalExit.BlockNumber) + voluntaryExitExecutionSlotsToPostpone)
		waitForNoAction(t, nil, nil, noActionTimeout)
		require.EqualValues(t, 3, blockByNumberCalls.Load())
	})

	t.Run("slot = block + executionPostpone, block = 2 - executing new duty, fetching block number", func(t *testing.T) {
		executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
		setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

		ticker.Send(phase0.Slot(newBlockExit.BlockNumber) + voluntaryExitExecutionSlotsToPostpone)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuties[2:3])
		require.EqualValues(t, 3, blockByNumberCalls.Load())
	})

	exitCh <- pastBlockExit

	t.Run("slot = block + executionPostpone + 1, block = 5 - executing past duty, fetching block number", func(t *testing.T) {
		executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
		setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

		ticker.Send(phase0.Slot(pastBlockExit.BlockNumber) + voluntaryExitExecutionSlotsToPostpone + 1)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuties[3:4])
		require.EqualValues(t, 4, blockByNumberCalls.Load())
	})

	cancel()
	close(exitCh)
	require.NoError(t, scheduler.Wait())
	ticker.WaitShutdown()
}

func TestVoluntaryExitHandler_HandleDuties_LateObservedExitWaitsPastFollowDistance(t *testing.T) {
	t.Parallel()

	exitCh := make(chan ExitDescriptor)
	handler := NewVoluntaryExitHandler(dutystore.NewVoluntaryExit(), exitCh)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)

	scheduler, ticker := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, time.Unix(0, 0), time.Second)

	require.NoError(t, scheduler.Start(ctx))

	create1to1BlockSlotMapping(scheduler)

	const blockNumber = uint64(1)

	lateObservedExit := ExitDescriptor{
		OwnValidator:   true,
		PubKey:         phase0.BLSPubKey{7, 8, 9},
		ValidatorIndex: phase0.ValidatorIndex(3),
		BlockNumber:    blockNumber,
	}
	lateObservationSlot := phase0.Slot(blockNumber) + phase0.Slot(executionclient.FollowDistance)
	// expectedDuty.Slot uses the shared duty slot — what gets signed and what
	// peers (including pre-#2851 ones) use to validate. The execution gate
	// (at blockNumber + voluntaryExitExecutionSlotsToPostpone) trips later
	// but does not affect what's on the wire.
	expectedDuty := []*spectypes.ValidatorDuty{{
		Type:           spectypes.BNRoleVoluntaryExit,
		PubKey:         lateObservedExit.PubKey,
		Slot:           phase0.Slot(blockNumber) + voluntaryExitDutySlotsToPostpone,
		ValidatorIndex: lateObservedExit.ValidatorIndex,
	}}

	executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
	setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

	waitForSlotN(scheduler.netCfg.Beacon, lateObservationSlot)
	exitCh <- lateObservedExit

	t.Run("slot = block + followDistance - no execution", func(t *testing.T) {
		ticker.Send(lateObservationSlot)
		waitForNoAction(t, nil, executeDutiesCall, noActionTimeout)
	})

	t.Run("slot = block + executionPostpone - execute", func(t *testing.T) {
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(blockNumber)+voluntaryExitExecutionSlotsToPostpone)
		ticker.Send(phase0.Slot(blockNumber) + voluntaryExitExecutionSlotsToPostpone)
		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, expectedDuty)
	})

	cancel()
	close(exitCh)
	require.NoError(t, scheduler.Wait())
	ticker.WaitShutdown()
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

	slot := scheduler.netCfg.EstimatedSlotAtTime(time.Unix(int64(header.Time), 0))
	require.EqualValues(t, blockNumber, slot)
}

// expectedExecutedVoluntaryExitDuties builds the duties we expect to see
// dispatched for the given descriptors. Slot is the shared duty slot — what
// gets signed and what peers validate — which intentionally differs from the
// (later) local execution slot at which we actually broadcast.
func expectedExecutedVoluntaryExitDuties(descriptors []ExitDescriptor) []*spectypes.ValidatorDuty {
	expectedDuties := make([]*spectypes.ValidatorDuty, 0, len(descriptors))
	for _, d := range descriptors {
		expectedDuties = append(expectedDuties, &spectypes.ValidatorDuty{
			Type:           spectypes.BNRoleVoluntaryExit,
			PubKey:         d.PubKey,
			Slot:           phase0.Slot(d.BlockNumber) + voluntaryExitDutySlotsToPostpone,
			ValidatorIndex: d.ValidatorIndex,
		})
	}
	return expectedDuties
}
