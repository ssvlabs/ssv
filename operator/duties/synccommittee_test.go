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

func setupSyncCommitteeDutiesMock(s *Scheduler, dutiesMap map[uint64][]*v1.SyncCommitteeDuty, currentSlot *SlotValue) chan struct{} {
	fetchDutiesCall := make(chan struct{})

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

	s.validatorController.(*mocks.MockValidatorController).EXPECT().ActiveIndices(gomock.Any(), gomock.Any()).DoAndReturn(
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

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SubmitSyncCommitteeSubscriptions(gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall
}

func expectedSyncCommitteeDuties(handler *SyncCommitteeHandler, duties []*v1.SyncCommitteeDuty, slot phase0.Slot) []*spectypes.Duty {
	expectedDuties := make([]*spectypes.Duty, 0)
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommittee))
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommitteeContribution))
	}
	return expectedDuties
}

func TestScheduler_SyncCommittee_Same_Period(t *testing.T) {
	handler := NewSyncCommitteeHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(0),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	period := uint64(0)

	dutiesMap := make(map[uint64][]*v1.SyncCommitteeDuty)
	dutiesMap[period] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         pubKey,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupSyncCommitteeDutiesMock(s, dutiesMap, currentSlot)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// STEP 2: expect sync committee duties to be executed at the same period
	currentSlot.SetSlot(phase0.Slot(1))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// STEP 3: expect sync committee duties to be executed at the last slot of the period
	currentSlot.SetSlot(s.network.Beacon.LastSlotOfSyncPeriod(period))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// STEP 4: expect no action to be taken as we are in the next period
	firstSlotOfNextPeriod := s.network.Beacon.GetEpochFirstSlot(s.network.Beacon.FirstEpochOfSyncPeriod(period + 1))
	currentSlot.SetSlot(firstSlotOfNextPeriod)
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Current_Next_Periods(t *testing.T) {
	handler := NewSyncCommitteeHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(256*32 - 49),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	period := uint64(0)

	dutiesMap := make(map[uint64][]*v1.SyncCommitteeDuty)
	dutiesMap[period] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         pubKey,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	dutiesMap[period+1] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupSyncCommitteeDutiesMock(s, dutiesMap, currentSlot)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// STEP 2: wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256*32 - 48))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// STEP 3: wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256*32 - 47))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// ...

	// STEP 4: new period, wait for sync committee duties to be executed
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period+1], currentSlot.GetSlot()))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Indices_Changed(t *testing.T) {
	handler := NewSyncCommitteeHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(256*32 - 3),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	period := uint64(1)

	dutiesMap := make(map[uint64][]*v1.SyncCommitteeDuty)
	dutiesMap[period] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         pubKey,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupSyncCommitteeDutiesMock(s, dutiesMap, currentSlot)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	s.indicesChg <- true
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	dutiesMap[period] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	}

	// STEP 3: wait for sync committee duties to be fetched again
	currentSlot.SetSlot(phase0.Slot(256*32 - 2))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: no action should be taken
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: The first assigned duty should not be executed, but the second one should
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_SyncCommittee_Reorg_Current(t *testing.T) {
	handler := NewSyncCommitteeHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(256*32 - 3),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	period := uint64(1)

	dutiesMap := make(map[uint64][]*v1.SyncCommitteeDuty)
	dutiesMap[period] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         pubKey,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupSyncCommitteeDutiesMock(s, dutiesMap, currentSlot)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.GetSlot(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	s.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.SetSlot(phase0.Slot(256*32 - 2))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.GetSlot(),
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	s.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for sync committee duties to be fetched again for the current epoch
	currentSlot.SetSlot(phase0.Slot(256*32 - 1))
	dutiesMap[period] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	}
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	currentSlot.SetSlot(phase0.Slot(256 * 32))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Early_Block(t *testing.T) {
	handler := NewSyncCommitteeHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(0),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	period := uint64(0)

	dutiesMap := make(map[uint64][]*v1.SyncCommitteeDuty)
	dutiesMap[period] = []*v1.SyncCommitteeDuty{
		{
			PubKey:         pubKey,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupSyncCommitteeDutiesMock(s, dutiesMap, currentSlot)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// STEP 2: expect sync committee duties to be executed at the same period
	currentSlot.SetSlot(phase0.Slot(1))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))

	// STEP 3: wait for sync committee duties to be executed faster than 1/3 of the slot duration
	startTime := time.Now()
	currentSlot.SetSlot(phase0.Slot(2))
	mockTicker.Send(currentSlot.GetSlot())

	// STEP 4: trigger head event (block arrival)
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot: currentSlot.GetSlot(),
		},
	}
	s.HandleHeadEvent(logger)(e)
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedSyncCommitteeDuties(handler, dutiesMap[period], currentSlot.GetSlot()))
	require.Less(t, time.Since(startTime), s.network.Beacon.SlotDurationSec()/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
