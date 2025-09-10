package duties

import (
	"context"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func setupProposerDutiesMock(s *Scheduler, dutiesMap *hashmap.Map[phase0.Epoch, []*eth2apiv1.ProposerDuty]) (chan struct{}, chan []*spectypes.ValidatorDuty) {
	fetchDutiesCall := make(chan struct{})
	executeDutiesCall := make(chan []*spectypes.ValidatorDuty)

	s.beaconNode.(*MockBeaconNode).EXPECT().ProposerDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
			fetchDutiesCall <- struct{}{}
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
				attestingShare := &types.SSVShare{
					Share: spectypes.Share{
						ValidatorIndex: index,
					},
					ActivationEpoch: 0,
					Liquidated:      false,
					Status:          eth2apiv1.ValidatorStateActiveOngoing,
				}
				proposerShares = append(proposerShares, attestingShare)
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
	expectedDuties := make([]*spectypes.ValidatorDuty, 0)
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, spectypes.BNRoleProposer))
	}
	return expectedDuties
}

func TestScheduler_Proposer_Same_Slot(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty]())
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(0),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for proposer duties to be fetched and executed at the same slot
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedProposerDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(0))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Proposer_Diff_Slots(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty]())
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for proposer duties to be fetched
	ticker.Send(phase0.Slot(0))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	ticker.Send(phase0.Slot(1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: wait for proposer duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedProposerDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(2))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// execute duty after two slots after the indices changed
func TestScheduler_Proposer_Indices_Changed(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty]())
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap)
	startScheduler(ctx, t, scheduler, schedulerPool)

	// STEP 1: wait for no action to be taken
	ticker.Send(phase0.Slot(0))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: wait for no action to be taken
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	ticker.Send(phase0.Slot(1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
		{
			PubKey:         phase0.BLSPubKey{1, 2, 5},
			Slot:           phase0.Slot(3),
			ValidatorIndex: phase0.ValidatorIndex(3),
		},
	})
	// no execution should happen in slot 1
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: wait for proposer duties to be fetched again
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	ticker.Send(phase0.Slot(2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 2
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: wait for proposer duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(3))
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[2]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(3))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Proposer_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty]())
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
	fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for proposer duties to be fetched
	ticker.Send(phase0.Slot(0))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.ProposerDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(3),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))

	// STEP 3: trigger a change in active indices in the same slot
	scheduler.indicesChg <- struct{}{}
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.ProposerDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 5},
		Slot:           phase0.Slot(4),
		ValidatorIndex: phase0.ValidatorIndex(3),
	}))

	// STEP 4: wait for proposer duties to be fetched again
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	ticker.Send(phase0.Slot(1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for proposer duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[0]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(2))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 6: wait for proposer duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(3))
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	expected = expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[1]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(3))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 7: wait for proposer duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(4))
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	expected = expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[2]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(4))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_Proposer_Reorg_Current(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty]())
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch+2)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch+2)
	fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch + 4),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for proposer duties to be fetched
	ticker.Send(phase0.Slot(testSlotsPerEpoch + 2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     testSlotsPerEpoch + 2,
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+3))
	ticker.Send(phase0.Slot(testSlotsPerEpoch + 3))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     testSlotsPerEpoch + 3,
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch + 5),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 5: wait for proposer duties to be fetched again for the current epoch.
	// The first assigned duty should not be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+4))
	ticker.Send(phase0.Slot(testSlotsPerEpoch + 4))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The second assigned duty should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+5))
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedProposerDuties(handler, duties)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testSlotsPerEpoch + 5))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_Proposer_Reorg_Current_Indices_Changed(t *testing.T) {
	t.Parallel()

	var (
		handler   = NewProposerHandler(dutystore.NewDuties[eth2apiv1.ProposerDuty]())
		dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.ProposerDuty]()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch+2)
	waitForSlotN(scheduler.beaconConfig, testSlotsPerEpoch+2)
	fetchDutiesCall, executeDutiesCall := setupProposerDutiesMock(scheduler, dutiesMap)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch + 4),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for proposer duties to be fetched
	ticker.Send(phase0.Slot(testSlotsPerEpoch + 2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     testSlotsPerEpoch + 2,
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+3))
	ticker.Send(phase0.Slot(testSlotsPerEpoch + 3))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     testSlotsPerEpoch + 3,
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.ProposerDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(testSlotsPerEpoch + 5),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 5: trigger a change in active indices in the same slot
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	dutiesMap.Set(phase0.Epoch(1), append(duties, &eth2apiv1.ProposerDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(testSlotsPerEpoch + 6),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 6: wait for proposer duties to be fetched again for the current epoch.
	// The first assigned duty should not be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+4))
	ticker.Send(phase0.Slot(testSlotsPerEpoch + 4))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The second assigned duty should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+5))
	duties, _ = dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[0]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testSlotsPerEpoch + 5))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 8: The second assigned duty should be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testSlotsPerEpoch+6))
	duties, _ = dutiesMap.Get(phase0.Epoch(1))
	expected = expectedExecutedProposerDuties(handler, []*eth2apiv1.ProposerDuty{duties[1]})
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testSlotsPerEpoch + 6))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
