package duties

import (
	"context"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/operator/duties/mocks"
	mocknetwork "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon/mocks"
)

func setupSyncCommitteeDutiesMock(s *Scheduler, dutiesMap map[uint64][]*v1.SyncCommitteeDuty) (chan struct{}, chan []*spectypes.Duty) {
	fetchDutiesCall := make(chan struct{})
	executeDutiesCall := make(chan []*spectypes.Duty)

	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedSyncCommitteePeriodAtEpoch(gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch) uint64 {
			return uint64(epoch) / s.network.Beacon.EpochsPerSyncCommitteePeriod()
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().FirstEpochOfSyncPeriod(gomock.Any()).DoAndReturn(
		func(period uint64) phase0.Epoch {
			return phase0.Epoch(period * s.network.Beacon.EpochsPerSyncCommitteePeriod())
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().LastSlotOfSyncPeriod(gomock.Any()).DoAndReturn(
		func(period uint64) phase0.Slot {
			lastEpoch := s.network.Beacon.FirstEpochOfSyncPeriod(period+1) - 1
			// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
			// as it will never be included, hence -1.
			return s.network.Beacon.GetEpochFirstSlot(lastEpoch+1) - 2
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().GetEpochFirstSlot(gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch) phase0.Slot {
			return phase0.Slot(uint64(epoch) * s.network.Beacon.SlotsPerEpoch())
		},
	).AnyTimes()

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
			fetchDutiesCall <- struct{}{}
			period := s.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			return dutiesMap[period], nil
		}).AnyTimes()

	s.validatorController.(*mocks.MockValidatorController).EXPECT().ActiveValidatorIndices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(logger *zap.Logger, epoch phase0.Epoch) []phase0.ValidatorIndex {
			uniqueIndices := make(map[phase0.ValidatorIndex]bool)

			period := s.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			for _, d := range dutiesMap[period] {
				uniqueIndices[d.ValidatorIndex] = true
			}

			indices := make([]phase0.ValidatorIndex, 0, len(uniqueIndices))
			for index := range uniqueIndices {
				indices = append(indices, index)
			}

			return indices
		}).AnyTimes()

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SubmitSyncCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func expectedExecutedSyncCommitteeDuties(handler *SyncCommitteeHandler, duties []*v1.SyncCommitteeDuty, slot phase0.Slot) []*spectypes.Duty {
	expectedDuties := make([]*spectypes.Duty, 0)
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommittee))
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommitteeContribution))
	}
	return expectedDuties
}

func TestScheduler_SyncCommittee_Same_Period(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler()
		currentSlot = &SlotValue{}
		dutiesMap   = make(map[uint64][]*v1.SyncCommitteeDuty)
	)
	currentSlot.SetSlot(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)

	dutiesMap[0] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	expected := expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	startTime := time.Now()
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// STEP 2: expect sync committee duties to be executed at the same period
	currentSlot.SetSlot(phase0.Slot(2))
	expected = expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: expect sync committee duties to be executed at the last slot of the period
	currentSlot.SetSlot(scheduler.network.Beacon.LastSlotOfSyncPeriod(0))
	expected = expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 4: expect no action to be taken as we are in the next period
	firstSlotOfNextPeriod := scheduler.network.Beacon.GetEpochFirstSlot(scheduler.network.Beacon.FirstEpochOfSyncPeriod(1))
	currentSlot.SetSlot(firstSlotOfNextPeriod)
	ticker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Current_Next_Periods(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler()
		currentSlot = &SlotValue{}
		dutiesMap   = make(map[uint64][]*v1.SyncCommitteeDuty)
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 49))
	scheduler, logger, ticker, timeout, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)

	dutiesMap[0] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	dutiesMap[1] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	}

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	expected := expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256*32 - 48))
	expected = expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256*32 - 47))
	expected = expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// ...

	// STEP 4: new period, wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	expected = expectedExecutedSyncCommitteeDuties(handler, dutiesMap[1], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Indices_Changed(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler()
		currentSlot = &SlotValue{}
		dutiesMap   = make(map[uint64][]*v1.SyncCommitteeDuty)
	)
	currentSlot.SetSlot(phase0.Slot(0))
	scheduler, logger, ticker, timeout, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.GetSlot())
	expected := expectedExecutedSyncCommitteeDuties(handler, dutiesMap[1], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	dutiesMap[1] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
		{
			PubKey:         phase0.BLSPubKey{1, 2, 5},
			ValidatorIndex: phase0.ValidatorIndex(3),
		},
	}
	// no execution should happen in slot 0
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for sync committee duties to be fetched again
	currentSlot.SetSlot(phase0.Slot(1))
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 1
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(2))
	expected = expectedExecutedSyncCommitteeDuties(handler, []*v1.SyncCommitteeDuty{dutiesMap[1][2]}, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler()
		currentSlot = &SlotValue{}
		dutiesMap   = make(map[uint64][]*v1.SyncCommitteeDuty)
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.GetSlot())
	expected := expectedExecutedSyncCommitteeDuties(handler, dutiesMap[1], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	dutiesMap[1] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	dutiesMap[1] = append(dutiesMap[1], &v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		ValidatorIndex: phase0.ValidatorIndex(2),
	})
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for sync committee duties to be fetched again
	currentSlot.SetSlot(phase0.Slot(256*32 - 2))
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: no action should be taken
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	expected = expectedExecutedSyncCommitteeDuties(handler, dutiesMap[1], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_SyncCommittee_Reorg_Current(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler()
		currentSlot = &SlotValue{}
		dutiesMap   = make(map[uint64][]*v1.SyncCommitteeDuty)
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)

	dutiesMap[1] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.GetSlot(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.SetSlot(phase0.Slot(256*32 - 2))
	ticker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.GetSlot(),
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap[1] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for sync committee duties to be fetched again for the current epoch
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	expected := expectedExecutedSyncCommitteeDuties(handler, dutiesMap[1], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed including indices change in the same slot
func TestScheduler_SyncCommittee_Reorg_Current_Indices_Changed(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler()
		currentSlot = &SlotValue{}
		dutiesMap   = make(map[uint64][]*v1.SyncCommitteeDuty)
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)

	dutiesMap[1] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.GetSlot(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.SetSlot(phase0.Slot(256*32 - 2))
	ticker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.GetSlot(),
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap[1] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	dutiesMap[1] = append(dutiesMap[1], &v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 5},
		ValidatorIndex: phase0.ValidatorIndex(3),
	})
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for sync committee duties to be fetched again for the current epoch
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second and the new from indices change should
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	expected := expectedExecutedSyncCommitteeDuties(handler, dutiesMap[1], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Early_Block(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler()
		currentSlot = &SlotValue{}
		dutiesMap   = make(map[uint64][]*v1.SyncCommitteeDuty)
	)
	currentSlot.SetSlot(phase0.Slot(0))
	scheduler, logger, ticker, timeout, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)

	dutiesMap[0] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expected := expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: expect sync committee duties to be executed at the same period
	currentSlot.SetSlot(phase0.Slot(1))
	expected = expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: wait for sync committee duties to be executed faster than 1/3 of the slot duration
	startTime := time.Now()
	currentSlot.SetSlot(phase0.Slot(2))
	expected = expectedExecutedSyncCommitteeDuties(handler, dutiesMap[0], currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())

	// STEP 4: trigger head event (block arrival)
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot: currentSlot.GetSlot(),
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)
	require.Less(t, time.Since(startTime), scheduler.network.Beacon.SlotDurationSec()/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
