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

func setupProposerDutiesMock(s *Scheduler, dutiesMap map[phase0.Epoch][]*v1.ProposerDuty) chan struct{} {
	fetchDutiesCall := make(chan struct{})

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().ProposerDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.ProposerDuty, error) {
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

	return fetchDutiesCall
}

func expectedProposerDuties(handler *ProposerHandler, duties []*v1.ProposerDuty) []*spectypes.Duty {
	expectedDuties := make([]*spectypes.Duty, 0)
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, spectypes.BNRoleProposer))
	}
	return expectedDuties
}

func TestScheduler_Proposer_Same_Slot(t *testing.T) {
	handler := NewProposerHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(0),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.ProposerDuty)
	dutiesMap[currentEpoch] = []*v1.ProposerDuty{
		{
			PubKey:         pubKey,
			Slot:           0,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 1
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupProposerDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for proposer duties to be fetched and executed at the same slot
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedProposerDuties(handler, dutiesMap[currentEpoch]))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Proposer_Diff_Slots(t *testing.T) {
	handler := NewProposerHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(0),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.ProposerDuty)
	dutiesMap[currentEpoch] = []*v1.ProposerDuty{
		{
			PubKey:         pubKey,
			Slot:           2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 1
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupProposerDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for proposer duties to be fetched
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.SetSlot(phase0.Slot(1))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for proposer duties to be executed
	currentSlot.SetSlot(phase0.Slot(2))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedProposerDuties(handler, dutiesMap[currentEpoch]))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Proposer_Indices_Changed(t *testing.T) {
	handler := NewProposerHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(0),
	}

	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.ProposerDuty)
	dutiesMap[currentEpoch] = []*v1.ProposerDuty{
		{
			PubKey:         pubKey,
			Slot:           2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 1
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupProposerDutiesMock(s, dutiesMap)

	timeout := 100 * time.Millisecond

	// STEP 1: wait for proposer duties to be fetched
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	s.indicesChg <- struct{}{}
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for proposer duties to be fetched again
	currentSlot.SetSlot(phase0.Slot(1))
	dutiesMap[currentEpoch] = []*v1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           2,
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	}
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for proposer duties to be executed
	currentSlot.SetSlot(phase0.Slot(2))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedProposerDuties(handler, dutiesMap[currentEpoch]))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_Proposer_Reorg_Current(t *testing.T) {
	handler := NewProposerHandler()
	currentSlot := &SlotValue{
		slot: phase0.Slot(34),
	}
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(1)

	dutiesMap := make(map[phase0.Epoch][]*v1.ProposerDuty)
	dutiesMap[currentEpoch] = []*v1.ProposerDuty{
		{
			PubKey:         pubKey,
			Slot:           36,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	expectedBufferSize := 1
	s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool := setupSchedulerAndMocks(t, handler, currentSlot, expectedBufferSize)
	fetchDutiesCall := setupProposerDutiesMock(s, dutiesMap)
	handleEventFunc := s.HandleHeadEvent(logger)

	timeout := 50 * time.Millisecond

	// STEP 1: wait for proposer duties to be fetched
	mockTicker.Send(currentSlot.GetSlot()) // slot = 34
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.GetSlot(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	handleEventFunc(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.SetSlot(phase0.Slot(35))
	mockTicker.Send(currentSlot.GetSlot())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.GetSlot(),
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	handleEventFunc(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for proposer duties to be fetched again for the current epoch.
	// The first assigned duty should not be executed
	currentSlot.SetSlot(phase0.Slot(36))
	dutiesMap[currentEpoch] = []*v1.ProposerDuty{
		{
			PubKey:         pubKey,
			Slot:           37,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The second assigned duty should be executed
	currentSlot.SetSlot(phase0.Slot(37))
	mockTicker.Send(currentSlot.GetSlot())
	waitForDutyExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expectedProposerDuties(handler, dutiesMap[currentEpoch]))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
