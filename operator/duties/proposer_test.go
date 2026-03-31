package duties

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func setupProposerDutiesMock(
	s *Scheduler,
	dutiesMap *hashmap.Map[phase0.Epoch, []*eth2apiv1.ProposerDuty],
	waitForDuties *SafeValue[bool],
) (chan struct{}, chan []*spectypes.ValidatorDuty) {
	// fetchDutiesCall relays/signals duty-fetch calls, it is buffered so that our test code can run in a single
	// go-routine (so that we don't need to worry about draining this channel to let the execution proceed). The
	// buffer size should be large enough for the test to not block.
	fetchDutiesCall := make(chan struct{}, 100)
	// executeDutiesCall is similar to fetchDutiesCall but signals the duty-executions.
	executeDutiesCall := make(chan []*spectypes.ValidatorDuty, 100)

	s.beaconNode.(*MockBeaconNode).EXPECT().ProposerDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			duties, _ := dutiesMap.Get(epoch)
			return duties, nil
		}).AnyTimes()

	getShares := func() []*types.SSVShare {
		var proposerShares []*types.SSVShare
		dutiesMap.Range(func(epoch phase0.Epoch, duties []*eth2apiv1.ProposerDuty) bool {
			uniqueIndices := make(map[phase0.ValidatorIndex]bool)

			for _, d := range duties {
				uniqueIndices[d.ValidatorIndex] = true
			}

			for index := range uniqueIndices {
				proposerShares = append(proposerShares, &types.SSVShare{
					Share: spectypes.Share{
						ValidatorIndex: index,
					},
					ActivationEpoch: 0,
					Liquidated:      false,
					Status:          eth2apiv1.ValidatorStateActiveOngoing,
				})
			}
			return true
		})

		return proposerShares
	}

	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfValidators().DoAndReturn(getShares).AnyTimes()
	s.validatorProvider.(*MockValidatorProvider).EXPECT().Validators().DoAndReturn(getShares).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func expectedExecutedProposerDuties(handler *ProposerHandler, duties []*eth2apiv1.ProposerDuty) []*spectypes.ValidatorDuty {
	expectedDuties := make([]*spectypes.ValidatorDuty, 0, len(duties))
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, spectypes.BNRoleProposer))
	}
	return expectedDuties
}

func TestScheduler_Proposer_Same_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler   = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		waitForSlotN(scheduler.beaconConfig, 1)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Proposer_Diff_Slots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(2))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Proposer_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken on slot 0
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: trigger a change in active indices & wait for proposer duties to be re-fetched on slot 0
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
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
		scheduler.indicesChg <- struct{}{}
		waitForDuties.Set(true)
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		// no other fetching or execution should happen on slot 0
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for proposer duties to be executed on slot 1
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[1]})
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))
		ticker.Send(phase0.Slot(1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)
		// no other fetching or execution should happen on slot 1
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Proposer_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for no action to be taken
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: trigger a change in active indices
		scheduler.indicesChg <- struct{}{}
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.ProposerDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(3),
			ValidatorIndex: phase0.ValidatorIndex(1),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger a change in active indices in the same slot
		scheduler.indicesChg <- struct{}{}
		duties, _ = dutiesMap.Get(phase0.Epoch(0))
		dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.ProposerDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(4),
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: wait for proposer duties to be fetched
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
		waitForDuties.Set(true)
		ticker.Send(phase0.Slot(2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 6: wait for proposer duties to be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(3))
		duties, _ = dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[0]})
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(3))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 7: wait for proposer duties to be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(4))
		duties, _ = dutiesMap.Get(phase0.Epoch(0))
		expected = expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[1]})
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(4))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed
func TestScheduler_Proposer_Reorg_Previous(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for proposer duties to be fetched (handle initial duties)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch)
		waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		ticker.Send(phase0.Slot(testSlotsPerEpoch))
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
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on previous dependent root change (same epoch, no epoch transition).
		// Proposer duties depend on the current duty dependent root, not the previous one,
		// so no refetch should happen for the current epoch.
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch + 1,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: wait for no action to be taken
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: The originally assigned duty should be executed (no refetch happened, original duties remain)
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+3))
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch + 3))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed and the indices changed the same slot
func TestScheduler_Proposer_Reorg_Previous_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for proposer duties to be fetched (handle initial duties)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch)
		waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		ticker.Send(phase0.Slot(testSlotsPerEpoch))
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
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on previous dependent root change (same epoch, no epoch transition).
		// Proposer duties depend on the current duty dependent root, not the previous one,
		// so no refetch should happen for the current epoch.
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch + 1,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 4),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: trigger indices change
		scheduler.indicesChg <- struct{}{}
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		dutiesMap.Set(phase0.Epoch(1), append(duties, &eth2apiv1.ProposerDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(testSlotsPerEpoch + 4),
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: wait for proposer duties to be re-fetched for the current epoch
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 7: The first assigned duty should not be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+3))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 3))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The second and new from indices change assigned duties should be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+4))
		duties, _ = dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch + 4))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed
func TestScheduler_Proposer_Reorg_Previous_Epoch_Transition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)

		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch*2-1)
		waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch*2-1)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for proposer duties to be fetched for the current epoch
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

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

		// STEP 3: Ticker with no action (currently at the Epoch Transition Slot, i.e the 1st slot of Epoch 2)
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch * 2,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// The HandleHeadEvent should set the CurrentDutyDependentRootChanged to true due to PreviousDutyDependentRoot
		// have been changed, allowing for refetching the duties in current Epoch
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: wait for proposer duties to be re-fetched for the current epoch
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: The first assigned duty should not be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 7: The second assigned duty should be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+3))
		duties, _ := dutiesMap.Get(phase0.Epoch(2))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 3))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed and the indices changed as well
func TestScheduler_Proposer_Reorg_Previous_Epoch_Transition_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch*2-1)
		waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch*2-1)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for proposer duties to be fetched for the current epoch
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

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

		// STEP 3: Ticker with no action (currently at the Epoch Transition Slot, i.e the 1st slot of Epoch 2)
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on epoch transition
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch * 2,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// The HandleHeadEvent should set the CurrentDutyDependentRootChanged to true due to PreviousDutyDependentRoot
		// have been changed, allowing for refetching the duties in current Epoch
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: trigger indices change
		scheduler.indicesChg <- struct{}{}
		duties, _ := dutiesMap.Get(phase0.Epoch(2))
		dutiesMap.Set(phase0.Epoch(2), append(duties, &eth2apiv1.ProposerDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: wait for proposer duties to be re-fetched for the current epoch
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 7: The first assigned duty should not be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The second assigned duty should be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+3))
		duties, _ = dutiesMap.Get(phase0.Epoch(2))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 3))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Proposer_Reorg_Current(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch+testSlotsPerEpoch/2)
		waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch+testSlotsPerEpoch/2)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(0 + testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for proposer duties to be fetched for the current epoch
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 2: trigger head event
		e := &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2,
				CurrentDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: tick with no duty-execution + duty-fetch for the next epoch
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on current dependent root change.
		// Proposer duties depend on the current duty dependent root, so a refetch should happen immediately.
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2 + 1,
				CurrentDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: wait for proposer duties to be fetched for the next epoch
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 6: skip to the next epoch
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+3))
		for slot := phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 3); slot < testSlotsPerEpoch*2; slot++ {
			ticker.Send(slot)
			waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
			waitForSlotN(scheduler.beaconConfig, slot+1)
		}

		// STEP 7: The first assigned duty should not be executed
		// slot = testSlotsPerEpoch*2
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The second assigned duty should be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+1))
		duties, _ := dutiesMap.Get(phase0.Epoch(2))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg current dependent root changed including indices change in the same slot
func TestScheduler_Proposer_Reorg_Current_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch+testSlotsPerEpoch/2)
		waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch+testSlotsPerEpoch/2)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(0 + testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for proposer duties to be fetched for the current epoch
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 2: trigger head event
		e := &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2,
				CurrentDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: tick with no duty-execution + duty-fetch for the next epoch
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on current dependent root change.
		// Proposer duties depend on the current duty dependent root, so a refetch should happen immediately.
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2 + 1,
				CurrentDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: trigger indices change & wait for:
		// - on the current slot, proposer duties to be re-fetched for the current epoch (due to indices change)
		// - on the next slot, proposer duties to be re-fetched for the next epoch (due to indices change)
		duties, _ := dutiesMap.Get(phase0.Epoch(2))
		dutiesMap.Set(phase0.Epoch(2), append(duties, &eth2apiv1.ProposerDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		scheduler.indicesChg <- struct{}{}
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+3))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 3))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+4))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 4))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 6: skip to the next epoch
		for slot := phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 5); slot < testSlotsPerEpoch*2; slot++ {
			waitForSlotN(scheduler.beaconConfig, slot)
			ticker.Send(slot)
			waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		}

		// STEP 7: The first assigned duty should not be executed
		// slot = testSlotsPerEpoch*2
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The second assigned duty should be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch*2+1))
		duties, _ = dutiesMap.Get(phase0.Epoch(2))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Proposer_Early_Block(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler   = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for proposer duties to be fetched (handle initial duties)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for no action to be taken
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for proposer duties to be executed faster than 1/3 of the slot duration when
		// Beacon head event is observed (block arrival)
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))
		slotStartTime := time.Now()
		ticker.Send(phase0.Slot(2))

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
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Proposer_Start_At_The_Last_Slot_Of_The_Epoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch-1)
		waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch-1)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for proposer duties to be fetched for the next epoch
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout) // next epoch fetch-call

		// STEP 2: wait for proposer duties to be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch))
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Proposer_Fetch_Execute_Next_Epoch_Duty(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch/2-3)
		waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch/2-3)
		fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for no action to be taken
		ticker.Send(phase0.Slot(testSlotsPerEpoch/2 - 3))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for no action to be taken
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch/2-2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch/2 - 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for duties to be fetched for the next epoch
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch/2-1))
		waitForDuties.Set(true)
		ticker.Send(phase0.Slot(testSlotsPerEpoch/2 - 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 3: wait for proposer duties to be executed
		waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch))
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedProposerDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}
