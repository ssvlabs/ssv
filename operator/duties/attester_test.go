package duties

import (
	"bytes"
	"context"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func setupAttesterDutiesMock(
	s *Scheduler,
	dutiesMap *hashmap.Map[phase0.Epoch, []*eth2apiv1.AttesterDuty],
	waitForDuties *SafeValue[bool],
) (chan struct{}, chan []*spectypes.ValidatorDuty) {
	// Buffered to avoid deadlocks during HandleInitialDuties (which runs synchronously
	// inside Scheduler.Start) when the test hasn't started receiving yet.
	fetchDutiesCall := make(chan struct{}, 8)
	executeDutiesCall := make(chan []*spectypes.ValidatorDuty)

	s.beaconNode.(*MockBeaconNode).EXPECT().AttesterDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			duties, _ := dutiesMap.Get(epoch)
			return duties, nil
		}).AnyTimes()

	getShares := func() []*types.SSVShare {
		var attestingShares []*types.SSVShare
		dutiesMap.Range(func(epoch phase0.Epoch, duties []*eth2apiv1.AttesterDuty) bool {
			uniqueIndices := make(map[phase0.ValidatorIndex]bool)

			for _, d := range duties {
				uniqueIndices[d.ValidatorIndex] = true
			}

			for index := range uniqueIndices {
				attestingShare := &types.SSVShare{
					Share: spectypes.Share{
						ValidatorIndex: index,
					},
					ActivationEpoch: epoch,
					Liquidated:      false,
					// this particular status is needed so that ActivationEpoch can be taken into consideration when checking the IsAttesting() condition.
					Status: eth2apiv1.ValidatorStatePendingQueued,
				}
				attestingShares = append(attestingShares, attestingShare)
			}
			return true
		})

		return attestingShares
	}

	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfValidators().DoAndReturn(getShares).AnyTimes()
	s.validatorProvider.(*MockValidatorProvider).EXPECT().Validator(gomock.Any()).DoAndReturn(
		func(pubKey []byte) (*types.SSVShare, bool) {
			var ssvShare *types.SSVShare
			var minEpoch phase0.Epoch
			dutiesMap.Range(func(epoch phase0.Epoch, duties []*eth2apiv1.AttesterDuty) bool {
				for _, duty := range duties {
					if bytes.Equal(duty.PubKey[:], pubKey) {
						ssvShare = &types.SSVShare{
							Share: spectypes.Share{
								ValidatorIndex: duty.ValidatorIndex,
							},
						}
						if epoch < minEpoch {
							minEpoch = epoch
							ssvShare.SetMinParticipationEpoch(epoch)
						}
						return true
					}
				}
				return true
			})

			if ssvShare != nil {
				return ssvShare, true
			}

			return nil, false
		},
	).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitBeaconCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func expectedExecutedAttesterDuties(handler *AttesterHandler, duties []*eth2apiv1.AttesterDuty) []*spectypes.ValidatorDuty {
	expectedDuties := make([]*spectypes.ValidatorDuty, 0)
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, spectypes.BNRoleAggregator))
	}
	return expectedDuties
}

// waitForFetchCount waits for exactly n fetch calls to occur (signaled on fetchDutiesCall)
// within the given timeout. It fails the test if fewer than n occur before timeout
// or if any executeDutiesCall arrives unexpectedly.
func waitForFetchCount(
	t *testing.T,
	fetchDutiesCall chan struct{},
	executeDutiesCall chan []*spectypes.ValidatorDuty,
	n int,
	timeout time.Duration,
) {
	deadline := time.After(timeout)
	got := 0
	for got < n {
		select {
		case <-fetchDutiesCall:
			got++
		case <-executeDutiesCall:
			require.FailNow(t, "unexpected execute duty call")
		case <-deadline:
			require.FailNowf(t, "timed out waiting for fetch calls", "wanted=%d got=%d", n, got)
		}
	}
}

// Regression: when starting near an epoch boundary, HandleInitialDuties should also
// prefetch next-epoch duties so the new epoch does not start empty.
func TestAttester_HandleInitialDuties_PrefetchNextEpoch_NearBoundary(t *testing.T) {
	t.Parallel()

	handler := NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
	dutiesMap := hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
	waitForDuties := &SafeValue[bool]{}

	// Start at the last slot of epoch 0 so initial run is "near boundary".
	startSlot := phase0.Slot(testSlotsPerEpoch - 1)

	// Duty executor expects a deadline on the parent context.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, _, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
	// Ensure wall-clock aligns with the start slot time.
	waitForSlotN(scheduler.beaconConfig, startSlot)

	// Provide at least one eligible validator so fetches actually occur.
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{{
		PubKey:         phase0.BLSPubKey{0xaa},
		Slot:           startSlot, // any slot in epoch 0
		ValidatorIndex: phase0.ValidatorIndex(1),
	}})
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{{
		PubKey:         phase0.BLSPubKey{0xbb},
		Slot:           startSlot + 1, // any slot in epoch 1
		ValidatorIndex: phase0.ValidatorIndex(2),
	}})

	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)

	// We want to observe both current-epoch and next-epoch fetches during HandleInitialDuties.
	waitForDuties.Set(true)

	// Start scheduler (synchronously calls HandleInitialDuties for handlers).
	startScheduler(ctx, t, scheduler, schedulerPool)

	// Expect 2 fetches: one for current epoch (0) and one prefetch for next epoch (1).
	waitForFetchCount(t, fetchDutiesCall, executeDutiesCall, 2, timeout)

	// No slot tick needed for this test; ensure no spurious executions happen immediately.
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// Cleanup.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// Regression: when entering a new epoch with an empty duty store (e.g., restart right
// before epoch rollover), the first tick in the new epoch should force a fetch for
// the current epoch.
func TestAttester_Tick_ForceFetch_WhenEpochMissing(t *testing.T) {
	t.Parallel()

	handler := NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
	dutiesMap := hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
	waitForDuties := &SafeValue[bool]{}

	// Start early in the epoch (not near boundary) to avoid the startup prefetch path.
	// This ensures epoch 1 remains missing in the store until the first tick of epoch 1.
	startSlot := phase0.Slot(1)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
	waitForSlotN(scheduler.beaconConfig, startSlot)

	// Prepare shares that will be eligible in epoch 1 so the tick-forced fetch will occur.
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{{
		PubKey:         phase0.BLSPubKey{0xcc},
		Slot:           phase0.Slot(testSlotsPerEpoch + 1),
		ValidatorIndex: phase0.ValidatorIndex(7),
	}})

	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)

	// Do not listen to initial fetches.
	waitForDuties.Set(false)
	startScheduler(ctx, t, scheduler, schedulerPool)

	// Advance into the first slot of the new epoch.
	newEpochFirstSlot := phase0.Slot(testSlotsPerEpoch)
	waitForSlotN(scheduler.beaconConfig, newEpochFirstSlot)

	// Now we want to observe a fetch triggered by the tick due to missing epoch in store.
	waitForDuties.Set(true)
	mockTicker.Send(newEpochFirstSlot)

	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// Cleanup.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// When boundary guards are disabled via env, the tick-forced fetch should not occur.
func TestAttester_Tick_ForceFetch_DisabledEnv_NoFetch(t *testing.T) {
	// Do not run in parallel to avoid env bleed across tests.
	t.Setenv("SSV_TEST_DISABLE_ATTESTER_BOUNDARY_GUARDS", "1")

	handler := NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
	dutiesMap := hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
	waitForDuties := &SafeValue[bool]{}

	startSlot := phase0.Slot(1)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
	waitForSlotN(scheduler.beaconConfig, startSlot)

	// No duties seeded for epoch 1; with guards disabled, no fetch should be triggered on tick.
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	waitForDuties.Set(false)
	startScheduler(ctx, t, scheduler, schedulerPool)

	newEpochFirstSlot := phase0.Slot(testSlotsPerEpoch)
	waitForSlotN(scheduler.beaconConfig, newEpochFirstSlot)
	waitForDuties.Set(true)
	mockTicker.Send(newEpochFirstSlot)
	// Expect no fetch to happen.
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Same_Slot(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
	)
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, ticker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	waitForSlotN(scheduler.beaconConfig, 1)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, &SafeValue[bool]{})
	startScheduler(ctx, t, scheduler, schedulerPool)

	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(1))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Diff_Slots(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
	)
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, ticker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, &SafeValue[bool]{})
	startScheduler(ctx, t, scheduler, schedulerPool)

	ticker.Send(phase0.Slot(0))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	ticker.Send(phase0.Slot(1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(2))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Indices_Changed(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	// STEP 1: wait for no action to be taken
	mockTicker.Send(phase0.Slot(0))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	// no execution should happen in slot 0
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(0),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(1),
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
		{
			PubKey:         phase0.BLSPubKey{1, 2, 5},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(3),
		},
	})

	// STEP 3: wait for attester duties to be fetched again
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 1
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: wait for attester duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[2]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(2))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	// STEP 1: wait for no action to be taken
	mockTicker.Send(phase0.Slot(0))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: wait for no action to be taken
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	mockTicker.Send(phase0.Slot(1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 3},
		Slot:           phase0.Slot(3),
		ValidatorIndex: phase0.ValidatorIndex(1),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger a change in active indices in the same slot
	scheduler.indicesChg <- struct{}{}
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(4),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 5: wait for attester duties to be fetched
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: wait for attester duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(3))
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[0]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(3))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 7: wait for attester duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(4))
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	expected = expectedExecutedAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[1]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(4))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Attester_Reorg_Previous_Epoch_Transition(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)

	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)

	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch*2-1)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch*2-1)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 - 1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      testSlotsPerEpoch*2 - 1,
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch * 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg on epoch transition
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      testSlotsPerEpoch * 2,
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+1))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 6: The first assigned duty should not be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 + 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 7: The second assigned duty should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+3))
	duties, _ := dutiesMap.Get(phase0.Epoch(2))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 + 3))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed and the indices changed as well
func TestScheduler_Attester_Reorg_Previous_Epoch_Transition_Indices_Changed(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch*2-1)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch*2-1)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 - 1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      testSlotsPerEpoch*2 - 1,
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch * 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg on epoch transition
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      testSlotsPerEpoch * 2,
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(2))
	dutiesMap.Set(phase0.Epoch(2), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 6: wait for attester duties to be fetched again for the current epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+1))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The first assigned duty should not be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 + 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 8: The second assigned duty should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+3))
	duties, _ = dutiesMap.Get(phase0.Epoch(2))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 + 3))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Attester_Reorg_Previous(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch + 3),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched (handle initial duties)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      testSlotsPerEpoch,
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+1))
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + 1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      testSlotsPerEpoch + 1,
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch + 4),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for no action to be taken
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 6: The first assigned duty should not be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+3))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + 3))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 7: The second assigned duty should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+4))
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + 4))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed and the indices changed the same slot
func TestScheduler_Attester_Reorg_Previous_Indices_Change_Same_Slot(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch + 3),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched (handle initial duties)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      testSlotsPerEpoch,
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+1))
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + 1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      testSlotsPerEpoch + 1,
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch + 4),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	dutiesMap.Set(phase0.Epoch(1), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(testSlotsPerEpoch + 4),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 6: wait for attester duties to be fetched again for the current epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + 2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The first assigned duty should not be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+3))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + 3))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 8: The second and new from indices change assigned duties should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+4))
	duties, _ = dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + 4))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_Attester_Reorg_Current(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch+testSlotsPerEpoch/2)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch+testSlotsPerEpoch/2)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch * 2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2,
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+1))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2 + 1,
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: skip to the next epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+3))
	for slot := phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 3); slot < testSlotsPerEpoch*2; slot++ {
		mockTicker.Send(slot)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		waitForSlotN(scheduler.beaconConfig, slot+1)
	}

	// STEP 7: The first assigned duty should not be executed
	// slot = testSlotsPerEpoch*2
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch * 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 8: The second assigned duty should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+1))
	duties, _ := dutiesMap.Get(phase0.Epoch(2))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed including indices change in the same slot
func TestScheduler_Attester_Reorg_Current_Indices_Changed(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch+testSlotsPerEpoch/2)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch+testSlotsPerEpoch/2)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch * 2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2,
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+1))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2 + 1,
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(2))
	dutiesMap.Set(phase0.Epoch(2), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 6: wait for attester duties to be fetched again for the next epoch due to indices change
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: skip to the next epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+3))
	for slot := phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 3); slot < testSlotsPerEpoch*2; slot++ {
		mockTicker.Send(slot)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		waitForSlotN(scheduler.beaconConfig, slot+1)
	}

	// STEP 8: The first assigned duty should not be executed
	// slot = testSlotsPerEpoch*2
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch * 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 9: The second assigned duty should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+1))
	duties, _ = dutiesMap.Get(phase0.Epoch(2))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Early_Block(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
	)
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched (handle initial duties)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, &SafeValue[bool]{})
	startScheduler(ctx, t, scheduler, schedulerPool)

	mockTicker.Send(phase0.Slot(0))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: wait for no action to be taken
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	mockTicker.Send(phase0.Slot(1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration when
	// Beacon head event is observed (block arrival)
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))
	slotStartTime := time.Now()
	mockTicker.Send(phase0.Slot(2))

	// STEP 4: trigger head event (block arrival)
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot: 2,
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)
	require.Less(t, time.Since(slotStartTime), scheduler.beaconConfig.SlotDuration/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Start_In_The_End_Of_The_Epoch(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch-1)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch-1)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for the next epoch
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch - 1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for attester duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch))
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Fetch_Execute_Next_Epoch_Duty(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
	)
	// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	scheduler, mockTicker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch/2-3)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch/2-3)
	fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for no action to be taken
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch/2 - 3))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: wait for no action to be taken
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch/2-2))
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch/2 - 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: wait for duties to be fetched for the next epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch/2-1))
	waitForDuties.Set(true)
	mockTicker.Send(phase0.Slot(testSlotsPerEpoch/2 - 1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for attester duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch))
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedAttesterDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(phase0.Slot(testSlotsPerEpoch))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
