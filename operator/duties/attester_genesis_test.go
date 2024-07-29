package duties

import (
	"context"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cornelk/hashmap"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func setupAttesterGenesisDutiesMock(
	s *Scheduler,
	dutiesMap *hashmap.Map[phase0.Epoch, []*eth2apiv1.AttesterDuty],
	waitForDuties *SafeValue[bool],
) (chan struct{}, chan []*genesisspectypes.Duty) {
	fetchDutiesCall := make(chan struct{})
	executeDutiesCall := make(chan []*genesisspectypes.Duty)

	s.beaconNode.(*MockBeaconNode).EXPECT().AttesterDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			duties, _ := dutiesMap.Get(epoch)
			return duties, nil
		}).AnyTimes()

	getShares := func(epoch phase0.Epoch) []*types.SSVShare {
		uniqueIndices := make(map[phase0.ValidatorIndex]bool)

		duties, _ := dutiesMap.Get(epoch)
		for _, d := range duties {
			uniqueIndices[d.ValidatorIndex] = true
		}

		shares := make([]*types.SSVShare, 0, len(uniqueIndices))
		for index := range uniqueIndices {
			share := &types.SSVShare{
				Share: spectypes.Share{
					ValidatorIndex: index,
				},
			}
			shares = append(shares, share)
		}

		return shares
	}
	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfParticipatingValidators(gomock.Any()).DoAndReturn(getShares).AnyTimes()
	s.validatorProvider.(*MockValidatorProvider).EXPECT().ParticipatingValidators(gomock.Any()).DoAndReturn(getShares).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitBeaconCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func expectedExecutedGenesisAttesterDuties(handler *AttesterHandler, duties []*eth2apiv1.AttesterDuty) []*genesisspectypes.Duty {
	expectedDuties := make([]*genesisspectypes.Duty, 0)
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toGenesisSpecDuty(d, genesisspectypes.BNRoleAttester))
		expectedDuties = append(expectedDuties, handler.toGenesisSpecDuty(d, genesisspectypes.BNRoleAggregator))
	}
	return expectedDuties
}

func TestScheduler_Attester_Genesis_Same_Slot(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	currentSlot.Set(phase0.Slot(1))

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Genesis_Diff_Slots(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	currentSlot.Set(phase0.Slot(0))

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	ticker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	currentSlot.Set(phase0.Slot(1))
	ticker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	currentSlot.Set(phase0.Slot(2))
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Genesis_Indices_Changed(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	currentSlot.Set(phase0.Slot(0))
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	// no execution should happen in slot 0
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
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
	currentSlot.Set(phase0.Slot(1))
	mockTicker.Send(currentSlot.Get())
	waitForDuties.Set(true)
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 1
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for attester duties to be executed
	currentSlot.Set(phase0.Slot(2))
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedGenesisAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[2]})
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Genesis_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	currentSlot.Set(phase0.Slot(0))
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.Set(phase0.Slot(1))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 3},
		Slot:           phase0.Slot(3),
		ValidatorIndex: phase0.ValidatorIndex(1),
	}))
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger a change in active indices in the same slot
	scheduler.indicesChg <- struct{}{}
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(4),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for attester duties to be fetched
	currentSlot.Set(phase0.Slot(2))
	mockTicker.Send(currentSlot.Get())
	waitForDuties.Set(true)
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: wait for attester duties to be executed
	currentSlot.Set(phase0.Slot(3))
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedGenesisAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[0]})
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 7: wait for attester duties to be executed
	currentSlot.Set(phase0.Slot(4))
	duties, _ = dutiesMap.Get(phase0.Epoch(0))
	expected = expectedExecutedGenesisAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[1]})
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Attester_Genesis_Reorg_Previous_Epoch_Transition(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	currentSlot.Set(phase0.Slot(63))
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(66),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	mockTicker.Send(currentSlot.Get())
	waitForDuties.Set(true)
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(64))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg on epoch transition
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(67),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot.Set(phase0.Slot(65))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed
	currentSlot.Set(phase0.Slot(66))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The second assigned duty should be executed
	currentSlot.Set(phase0.Slot(67))
	duties, _ := dutiesMap.Get(phase0.Epoch(2))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed and the indices changed as well
func TestScheduler_Attester_Genesis_Reorg_Previous_Epoch_Transition_Indices_Changed(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	currentSlot.Set(phase0.Slot(63))
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(66),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	mockTicker.Send(currentSlot.Get())
	waitForDuties.Set(true)
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(64))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg on epoch transition
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(67),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(2))
	dutiesMap.Set(phase0.Epoch(2), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(67),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: wait for attester duties to be fetched again for the current epoch
	currentSlot.Set(phase0.Slot(65))
	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The first assigned duty should not be executed
	currentSlot.Set(phase0.Slot(66))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 8: The second assigned duty should be executed
	currentSlot.Set(phase0.Slot(67))
	duties, _ = dutiesMap.Get(phase0.Epoch(2))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Attester_Genesis_Reorg_Previous(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(35),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	currentSlot.Set(phase0.Slot(32))

	// STEP 1: wait for attester duties to be fetched (handle initial duties)
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(33))
	waitForDuties.Set(true)
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(36),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for no action to be taken
	currentSlot.Set(phase0.Slot(34))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed
	currentSlot.Set(phase0.Slot(35))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The second assigned duty should be executed
	currentSlot.Set(phase0.Slot(36))
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed and the indices changed the same slot
func TestScheduler_Attester_Genesis_Reorg_Previous_Indices_Change_Same_Slot(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(35),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	currentSlot.Set(phase0.Slot(32))

	// STEP 1: wait for attester duties to be fetched (handle initial duties)
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(33))
	waitForDuties.Set(true)
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(36),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	dutiesMap.Set(phase0.Epoch(1), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(36),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: wait for attester duties to be fetched again for the current epoch
	currentSlot.Set(phase0.Slot(34))
	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The first assigned duty should not be executed
	currentSlot.Set(phase0.Slot(35))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 8: The second and new from indices change assigned duties should be executed
	currentSlot.Set(phase0.Slot(36))
	duties, _ = dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_Attester_Genesis_Reorg_Current(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	currentSlot.Set(phase0.Slot(47))
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(64),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	waitForDuties.Set(true)
	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(48))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(65),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot.Set(phase0.Slot(49))
	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: skip to the next epoch
	currentSlot.Set(phase0.Slot(50))
	for slot := currentSlot.Get(); slot < 64; slot++ {
		mockTicker.Send(slot)
		waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
		currentSlot.Set(slot + 1)
	}

	// STEP 7: The first assigned duty should not be executed
	// slot = 64
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 8: The second assigned duty should be executed
	currentSlot.Set(phase0.Slot(65))
	duties, _ := dutiesMap.Get(phase0.Epoch(2))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed including indices change in the same slot
func TestScheduler_Attester_Genesis_Reorg_Current_Indices_Changed(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	currentSlot.Set(phase0.Slot(47))
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(64),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	waitForDuties.Set(true)
	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(48))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(65),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(phase0.Epoch(2))
	dutiesMap.Set(phase0.Epoch(2), append(duties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(65),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: wait for attester duties to be fetched again for the next epoch due to indices change
	currentSlot.Set(phase0.Slot(49))
	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: skip to the next epoch
	currentSlot.Set(phase0.Slot(50))
	for slot := currentSlot.Get(); slot < 64; slot++ {
		mockTicker.Send(slot)
		waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
		currentSlot.Set(slot + 1)
	}

	// STEP 8: The first assigned duty should not be executed
	// slot = 64
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 9: The second assigned duty should be executed
	currentSlot.Set(phase0.Slot(65))
	duties, _ = dutiesMap.Get(phase0.Epoch(2))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Genesis_Early_Block(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	currentSlot.Set(phase0.Slot(0))

	// STEP 1: wait for attester duties to be fetched (handle initial duties)
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.Set(phase0.Slot(1))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration
	startTime := time.Now()
	currentSlot.Set(phase0.Slot(2))
	mockTicker.Send(currentSlot.Get())
	duties, _ := dutiesMap.Get(phase0.Epoch(0))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	// STEP 4: trigger head event (block arrival)
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot: currentSlot.Get(),
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)
	require.Less(t, time.Since(startTime), scheduler.network.Beacon.SlotDurationSec()/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Genesis_Start_In_The_End_Of_The_Epoch(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	currentSlot.Set(phase0.Slot(31))
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(32),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for the next epoch
	waitForDuties.Set(true)
	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for attester duties to be executed
	currentSlot.Set(phase0.Slot(32))
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Genesis_Fetch_Execute_Next_Epoch_Duty(t *testing.T) {
	var (
		handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty]())
		currentSlot   = &SafeValue[phase0.Slot]{}
		dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = goclient.FarFutureEpoch
	)
	currentSlot.Set(phase0.Slot(13))
	scheduler, logger, mockTicker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupAttesterGenesisDutiesMock(scheduler, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(32),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for no action to be taken
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.Set(phase0.Slot(14))
	mockTicker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for duties to be fetched for the next epoch
	currentSlot.Set(phase0.Slot(15))
	waitForDuties.Set(true)
	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for attester duties to be executed
	currentSlot.Set(phase0.Slot(32))
	duties, _ := dutiesMap.Get(phase0.Epoch(1))
	expected := expectedExecutedGenesisAttesterDuties(handler, duties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCall, len(expected))

	mockTicker.Send(currentSlot.Get())
	waitForGenesisDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
