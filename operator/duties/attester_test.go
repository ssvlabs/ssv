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
)

func setupAttesterDutiesMock(s *Scheduler, dutiesMap map[phase0.Epoch][]*v1.AttesterDuty) chan struct{} {
	fetchDutiesCall := make(chan struct{})

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().AttesterDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.AttesterDuty, error) {
			fetchDutiesCall <- struct{}{}
			return dutiesMap[epoch], nil
		}).AnyTimes()

	s.validatorController.(*mocks.MockValidatorController).EXPECT().ActiveValidatorIndices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(logger *zap.Logger, epoch phase0.Epoch) []phase0.ValidatorIndex {
			uniqueIndices := make(map[phase0.ValidatorIndex]bool)

			for _, d := range dutiesMap[epoch] {
				uniqueIndices[d.ValidatorIndex] = true
			}

			indices := make([]phase0.ValidatorIndex, 0, len(uniqueIndices))
			for index := range uniqueIndices {
				indices = append(indices, index)
			}

			return indices
		}).AnyTimes()

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SubscribeToCommitteeSubnet(gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall
}

func expectedAttesterDuties(handler *AttesterHandler, duties []*v1.AttesterDuty) []*spectypes.Duty {
	expectedDuties := make([]*spectypes.Duty, 0)
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, spectypes.BNRoleAttester))
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, spectypes.BNRoleAggregator))
	}
	return expectedDuties
}

func TestScheduler_Attester_Same_Slot(t *testing.T) {
	handler := NewAttesterHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(1),
	}
	currentEpoch := phase0.Epoch(0)
	pubKey := phase0.BLSPubKey{1, 2, 3}

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           1,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupAttesterDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	startTime := time.Now()
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedAttesterDuties(handler, dutiesMap[currentEpoch]))

	// validate the 1/3 of the slot waiting time
	require.Less(t, s.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Diff_Slots(t *testing.T) {
	handler := NewAttesterHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(0),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupAttesterDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.SetSlot(phase0.Slot(1))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for attester duties to be executed
	//startTime := time.Now()
	currentSlot.SetSlot(phase0.Slot(2))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedAttesterDuties(handler, dutiesMap[currentEpoch]))
	//require.Less(t, s.network.Beacon.SlotDurationSec()/3, time.Now().Sub(startTime))
	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Indices_Changed(t *testing.T) {
	handler := NewAttesterHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(0),
	}

	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 4
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupAttesterDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	s.indicesChg <- true
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for attester duties to be fetched again
	currentSlot.SetSlot(phase0.Slot(1))
	dutiesMap[currentEpoch] = append(dutiesMap[currentEpoch], &v1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           2,
		ValidatorIndex: phase0.ValidatorIndex(2),
	})
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for attester duties to be executed
	currentSlot.SetSlot(phase0.Slot(2))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedAttesterDuties(handler, dutiesMap[currentEpoch]))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Attester_Reorg_Previous_Epoch_Transition(t *testing.T) {
	handler := NewAttesterHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(63),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(1)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch+1] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           64 + 2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupAttesterDutiesMock(s, dutiesMap)

	timeout := 120 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched for current and next epoch
	mockTicker.Send(currentSlot.GetSlot()) // slot = 63
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                      currentSlot.GetSlot(),
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	s.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.SetSlot(phase0.Slot(64))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg on epoch transition
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                      currentSlot.GetSlot(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	s.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot.SetSlot(phase0.Slot(65))
	dutiesMap[currentEpoch+1] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           64 + 3,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed
	currentSlot.SetSlot(phase0.Slot(66))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The second assigned duty should be executed
	currentSlot.SetSlot(phase0.Slot(67))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedAttesterDuties(handler, dutiesMap[currentEpoch+1]))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Attester_Reorg_Previous(t *testing.T) {
	handler := NewAttesterHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(32),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(1)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           32 + 3,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupAttesterDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot.GetSlot()) // slot = 32
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                      currentSlot.GetSlot(),
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	s.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.SetSlot(phase0.Slot(33))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                      currentSlot.GetSlot(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	s.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot.SetSlot(phase0.Slot(34))
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           32 + 4,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed
	currentSlot.SetSlot(phase0.Slot(35))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The second assigned duty should be executed
	currentSlot.SetSlot(phase0.Slot(36))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedAttesterDuties(handler, dutiesMap[currentEpoch]))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_Attester_Reorg_Current(t *testing.T) {
	handler := NewAttesterHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(47),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(1)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch+1] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           64,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupAttesterDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot.GetSlot()) // slot = 47
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
	currentSlot.SetSlot(phase0.Slot(48))
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

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot.SetSlot(phase0.Slot(49))
	dutiesMap[currentEpoch+1] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           65,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: skip to the next epoch
	currentSlot.SetSlot(phase0.Slot(50))
	for slot := currentSlot.GetSlot(); slot < 64; slot++ {
		mockTicker.Send(slot)
		waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
		currentSlot.SetSlot(slot + 1)
	}

	// STEP 7: The first assigned duty should not be executed
	// slot = 64
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 8: The second assigned duty should be executed
	currentSlot.SetSlot(phase0.Slot(65))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedAttesterDuties(handler, dutiesMap[currentEpoch+1]))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Early_Block(t *testing.T) {
	handler := NewAttesterHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(0),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 2
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupAttesterDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.SetSlot(phase0.Slot(1))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration
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
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedAttesterDuties(handler, dutiesMap[currentEpoch]))
	require.Less(t, time.Since(startTime), s.network.Beacon.SlotDurationSec()/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
