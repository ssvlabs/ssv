package duties

import (
	"context"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/operator/duties/mocks"
	mocknetwork "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon/mocks"
)

func setupSyncCommitteeDutiesMock(s *Scheduler, dutiesMap *hashmap.Map[uint64, []*v1.SyncCommitteeDuty]) (chan struct{}, chan []*spectypes.Duty) {
	fetchDutiesCall := make(chan struct{})
	executeDutiesCall := make(chan []*spectypes.Duty)

	s.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().EstimatedSyncCommitteePeriodAtEpoch(gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch) uint64 {
			return uint64(epoch) / s.network.Beacon.EpochsPerSyncCommitteePeriod()
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().FirstEpochOfSyncPeriod(gomock.Any()).DoAndReturn(
		func(period uint64) phase0.Epoch {
			return phase0.Epoch(period * s.network.Beacon.EpochsPerSyncCommitteePeriod())
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().LastSlotOfSyncPeriod(gomock.Any()).DoAndReturn(
		func(period uint64) phase0.Slot {
			lastEpoch := s.network.Beacon.FirstEpochOfSyncPeriod(period+1) - 1
			// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
			// as it will never be included, hence -1.
			return s.network.Beacon.GetEpochFirstSlot(lastEpoch+1) - 2
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().GetEpochFirstSlot(gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch) phase0.Slot {
			return phase0.Slot(uint64(epoch) * s.network.Beacon.SlotsPerEpoch())
		},
	).AnyTimes()

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
			fetchDutiesCall <- struct{}{}
			period := s.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			duties, _ := dutiesMap.Get(period)
			return duties, nil
		}).AnyTimes()

	getDuties := func(epoch phase0.Epoch) []phase0.ValidatorIndex {
		uniqueIndices := make(map[phase0.ValidatorIndex]bool)

		period := s.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
		duties, _ := dutiesMap.Get(period)
		for _, d := range duties {
			uniqueIndices[d.ValidatorIndex] = true
		}

		indices := make([]phase0.ValidatorIndex, 0, len(uniqueIndices))
		for index := range uniqueIndices {
			indices = append(indices, index)
		}

		return indices
	}
	s.validatorController.(*mocks.MockValidatorController).EXPECT().CommitteeActiveIndices(gomock.Any()).DoAndReturn(getDuties).AnyTimes()
	s.validatorController.(*mocks.MockValidatorController).EXPECT().AllActiveIndices(gomock.Any()).DoAndReturn(getDuties).AnyTimes()

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
		handler     = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot = &SlotValue{}
		dutiesMap   = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
	)
	currentSlot.SetSlot(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)
	startFn()

	dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	startTime := time.Now()
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// STEP 2: expect sync committee duties to be executed at the same period
	currentSlot.SetSlot(phase0.Slot(2))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: expect sync committee duties to be executed at the last slot of the period
	currentSlot.SetSlot(scheduler.network.Beacon.LastSlotOfSyncPeriod(0))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
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
		handler     = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot = &SlotValue{}
		dutiesMap   = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 49))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)
	startFn()

	dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256*32 - 48))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256*32 - 47))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// ...

	// STEP 4: new period, wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	duties, _ = dutiesMap.Get(1)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Indices_Changed(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot = &SlotValue{}
		dutiesMap   = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)
	startFn()

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched for next period
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(1)
	dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for sync committee duties to be fetched again
	currentSlot.SetSlot(phase0.Slot(256*32 - 2))
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: no action should be taken
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: execute duties
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot = &SlotValue{}
		dutiesMap   = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(1)
	dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for sync committee duties to be fetched again
	currentSlot.SetSlot(phase0.Slot(256*32 - 2))
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: no action should be taken
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
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
		handler     = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot = &SlotValue{}
		dutiesMap   = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)
	startFn()

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.GetSlot())
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
	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for sync committee duties to be fetched again for the current epoch
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	duties, _ := dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
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
		handler     = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot = &SlotValue{}
		dutiesMap   = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
	)
	currentSlot.SetSlot(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)
	startFn()

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.GetSlot())
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
	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(1)
	dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 5},
		ValidatorIndex: phase0.ValidatorIndex(3),
	}))
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for sync committee duties to be fetched again for the current epoch
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second and the new from indices change should
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Early_Block(t *testing.T) {
	var (
		handler     = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot = &SlotValue{}
		dutiesMap   = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
	)
	currentSlot.SetSlot(phase0.Slot(0))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, handler, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, dutiesMap)
	startFn()

	dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: expect sync committee duties to be executed at the same period
	currentSlot.SetSlot(phase0.Slot(1))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.GetSlot())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: wait for sync committee duties to be executed faster than 1/3 of the slot duration
	startTime := time.Now()
	currentSlot.SetSlot(phase0.Slot(2))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.GetSlot())
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
