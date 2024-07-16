package duties

import (
	"context"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cornelk/hashmap"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	mocknetwork "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/mocks"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func setupSyncCommitteeDutiesMock(
	s *Scheduler,
	activeShares []*ssvtypes.SSVShare,
	dutiesMap *hashmap.Map[uint64, []*v1.SyncCommitteeDuty],
	waitForDuties *SafeValue[bool],
) (chan struct{}, chan []*spectypes.BeaconDuty) {
	fetchDutiesCall := make(chan struct{})
	executeDutiesCall := make(chan []*spectypes.BeaconDuty)

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

	s.beaconNode.(*MockBeaconNode).EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			period := s.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			duties, _ := dutiesMap.Get(period)
			return duties, nil
		}).AnyTimes()

	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfParticipatingValidators(gomock.Any()).Return(activeShares).AnyTimes()
	s.validatorProvider.(*MockValidatorProvider).EXPECT().ParticipatingValidators(gomock.Any()).Return(activeShares).AnyTimes()

	s.validatorController.(*MockValidatorController).EXPECT().AllActiveIndices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch, afterInit bool) []phase0.ValidatorIndex {
			return indicesFromShares(activeShares)
		}).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitSyncCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func expectedExecutedSyncCommitteeDuties(handler *SyncCommitteeHandler, duties []*v1.SyncCommitteeDuty, slot phase0.Slot) []*spectypes.BeaconDuty {
	expectedDuties := make([]*spectypes.BeaconDuty, 0)
	for _, d := range duties {
		if !handler.network.AlanForked(slot) {
			expectedDuties = append(expectedDuties, handler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommittee))
		}
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommitteeContribution))
	}
	return expectedDuties
}

func TestScheduler_SyncCommittee_Same_Period(t *testing.T) {
	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = phase0.Epoch(0)
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched (handle initial duties)
	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startFn()

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: expect sync committee duties to be executed at the same period
	currentSlot.Set(phase0.Slot(2))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: expect sync committee duties to be executed at the last slot of the period
	currentSlot.Set(scheduler.network.Beacon.LastSlotOfSyncPeriod(0))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 4: expect no action to be taken as we are in the next period
	firstSlotOfNextPeriod := scheduler.network.Beacon.GetEpochFirstSlot(scheduler.network.Beacon.FirstEpochOfSyncPeriod(1))
	currentSlot.Set(firstSlotOfNextPeriod)
	ticker.Send(currentSlot.Get())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Current_Next_Periods(t *testing.T) {
	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = phase0.Epoch(0)
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 1,
				},
			},
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 2,
				},
			},
		}
	)
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

	// STEP 1: wait for sync committee duties to be fetched (handle initial duties)
	currentSlot.Set(phase0.Slot(256*32 - 49))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startFn()

	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: wait for sync committee duties to be executed
	currentSlot.Set(phase0.Slot(256*32 - 48))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: wait for sync committee duties to be executed
	currentSlot.Set(phase0.Slot(256*32 - 47))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// ...

	// STEP 4: new period, wait for sync committee duties to be executed
	currentSlot.Set(phase0.Slot(256 * 32))
	duties, _ = dutiesMap.Get(1)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Indices_Changed(t *testing.T) {
	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = phase0.Epoch(0)
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 1,
				},
			},
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 2,
				},
			},
		}
	)
	currentSlot.Set(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched for next period
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
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
	currentSlot.Set(phase0.Slot(256*32 - 2))
	ticker.Send(currentSlot.Get())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: no action should be taken
	currentSlot.Set(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.Get())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: execute duties
	currentSlot.Set(phase0.Slot(256 * 32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = phase0.Epoch(0)
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 1,
				},
			},
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 2,
				},
			},
		}
	)
	currentSlot.Set(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.Get())
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
	currentSlot.Set(phase0.Slot(256*32 - 2))
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: no action should be taken
	currentSlot.Set(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.Get())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	currentSlot.Set(phase0.Slot(256 * 32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_SyncCommittee_Reorg_Current(t *testing.T) {
	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = phase0.Epoch(0)
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 1,
				},
			},
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 2,
				},
			},
		}
	)
	currentSlot.Set(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(256*32 - 2))
	ticker.Send(currentSlot.Get())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.Get(),
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
	currentSlot.Set(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.Get())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	currentSlot.Set(phase0.Slot(256 * 32))
	duties, _ := dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed including indices change in the same slot
func TestScheduler_SyncCommittee_Reorg_Current_Indices_Changed(t *testing.T) {
	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = phase0.Epoch(0)
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 1,
				},
			},
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 2,
				},
			},
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 3,
				},
			},
		}
	)
	currentSlot.Set(phase0.Slot(256*32 - 3))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startFn()

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(256*32 - 2))
	ticker.Send(currentSlot.Get())
	waitForNoAction(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot.Get(),
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
	currentSlot.Set(phase0.Slot(256*32 - 1))
	ticker.Send(currentSlot.Get())
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second and the new from indices change should
	currentSlot.Set(phase0.Slot(256 * 32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Early_Block(t *testing.T) {
	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		forkEpoch     = phase0.Epoch(0)
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	currentSlot.Set(phase0.Slot(0))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startFn()

	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: expect sync committee duties to be executed at the same period
	currentSlot.Set(phase0.Slot(1))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: wait for sync committee duties to be executed faster than 1/3 of the slot duration
	startTime := time.Now()
	currentSlot.Set(phase0.Slot(2))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, currentSlot.Get())
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(currentSlot.Get())

	// STEP 4: trigger head event (block arrival)
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot: currentSlot.Get(),
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, expected)
	require.Greater(t, time.Since(startTime), scheduler.network.Beacon.SlotDurationSec()/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
