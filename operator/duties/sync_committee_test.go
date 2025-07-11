package duties

import (
	"bytes"
	"context"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func setupSyncCommitteeDutiesMock(
	s *Scheduler,
	activeShares []*ssvtypes.SSVShare,
	dutiesMap *hashmap.Map[uint64, []*v1.SyncCommitteeDuty],
	waitForDuties *SafeValue[bool],
) (chan struct{}, chan []*spectypes.ValidatorDuty) {
	fetchDutiesCall := make(chan struct{})
	executeDutiesCall := make(chan []*spectypes.ValidatorDuty)

	s.beaconNode.(*MockBeaconNode).EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			period := s.beaconConfig.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			duties, _ := dutiesMap.Get(period)
			return duties, nil
		}).AnyTimes()

	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfParticipatingValidators(gomock.Any()).Return(activeShares).MinTimes(1)
	s.validatorProvider.(*MockValidatorProvider).EXPECT().Validator(gomock.Any()).DoAndReturn(
		func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
			var ssvShare *ssvtypes.SSVShare
			var minEpoch phase0.Epoch
			dutiesMap.Range(func(period uint64, duties []*v1.SyncCommitteeDuty) bool {
				for _, duty := range duties {
					if bytes.Equal(duty.PubKey[:], pubKey) {
						ssvShare = &ssvtypes.SSVShare{
							Share: spectypes.Share{
								ValidatorIndex: duty.ValidatorIndex,
							},
						}
						firstEpoch := s.beaconConfig.FirstEpochOfSyncPeriod(period)
						if firstEpoch < minEpoch {
							minEpoch = firstEpoch
							ssvShare.SetMinParticipationEpoch(firstEpoch)
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

	s.validatorController.(*MockValidatorController).EXPECT().FilterIndices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(afterInit bool, filter func(s *ssvtypes.SSVShare) bool) []phase0.ValidatorIndex {
			var filteredShares []*ssvtypes.SSVShare
			for _, share := range activeShares {
				if filter(share) {
					filteredShares = append(filteredShares, share)
				}
			}
			return indicesFromShares(filteredShares)
		}).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitSyncCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func expectedExecutedSyncCommitteeDuties(handler *SyncCommitteeHandler, duties []*v1.SyncCommitteeDuty, slot phase0.Slot) []*spectypes.ValidatorDuty {
	expectedDuties := make([]*spectypes.ValidatorDuty, 0)
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommitteeContribution))
	}
	return expectedDuties
}

func TestScheduler_SyncCommittee_Same_Period(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		waitForDuties = &SafeValue[bool]{}
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = eligibleShares()
	)
	dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched (handle initial duties)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, 0, 5*time.Millisecond, testEpochsPerSCPeriod)
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, 1)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(1))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: expect sync committee duties to be executed at the same period
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, 2)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(2))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: expect sync committee duties to be executed at the last slot of the period
	waitForSlotN(scheduler.beaconConfig, scheduler.beaconConfig.LastSlotOfSyncPeriod(0))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, scheduler.beaconConfig.LastSlotOfSyncPeriod(0))
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(scheduler.beaconConfig.LastSlotOfSyncPeriod(0))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 4: expect no action to be taken as we are in the next period
	firstSlotOfNextPeriod := scheduler.beaconConfig.GetEpochFirstSlot(scheduler.beaconConfig.FirstEpochOfSyncPeriod(1))
	waitForSlotN(scheduler.beaconConfig, firstSlotOfNextPeriod)
	ticker.Send(firstSlotOfNextPeriod)
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Current_Next_Periods(t *testing.T) {
	t.Parallel()

	var (
		handler        = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		waitForDuties  = &SafeValue[bool]{}
		dutiesMap      = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		eligibleShares = eligibleShares()
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
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*32-49, 10*time.Millisecond, testEpochsPerSCPeriod)
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-49))
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, eligibleShares, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*32-49)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 49))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: wait for sync committee duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-48))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*32-48)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 48))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: wait for sync committee duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-47))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*32-47)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 47))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// ...

	// STEP 4: new period, wait for sync committee duties to be executed
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32))
	duties, _ = dutiesMap.Get(1)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*32)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testEpochsPerSCPeriod * 32))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Indices_Changed(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		waitForDuties = &SafeValue[bool]{}
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = eligibleShares()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*32-3, testSlotDuration, testEpochsPerSCPeriod)
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-3))
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched for next period
	waitForDuties.Set(true)
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 3))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(1)
	dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: wait for sync committee duties to be fetched again
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-2))
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: no action should be taken
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-1))
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 5: execute duties
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*32)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testEpochsPerSCPeriod * 32))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		waitForDuties = &SafeValue[bool]{}
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = eligibleShares()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*32-3, testSlotDuration, testEpochsPerSCPeriod)
	waitForSlotN(scheduler.beaconConfig, testEpochsPerSCPeriod*32-3)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	// STEP 1: wait for no action to be taken
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 3))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(1)
	dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: wait for sync committee duties to be fetched again
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-2))
	waitForDuties.Set(true)
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 2))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: no action should be taken
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-1))
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 1))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*32)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testEpochsPerSCPeriod * 32))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_SyncCommittee_Reorg_Current(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		waitForDuties = &SafeValue[bool]{}
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = eligibleShares()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*32-3, testSlotDuration, testEpochsPerSCPeriod)
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-3))
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	waitForDuties.Set(true)
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 3))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     testEpochsPerSCPeriod*32 - 3,
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(e.Data.(*v1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-2))
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     testEpochsPerSCPeriod*32 - 2,
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})
	scheduler.HandleHeadEvent()(e.Data.(*v1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 5: wait for sync committee duties to be fetched again for the current epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-1))
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second one should
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32))
	duties, _ := dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*32)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testEpochsPerSCPeriod * 32))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed including indices change in the same slot
func TestScheduler_SyncCommittee_Reorg_Current_Indices_Changed(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		waitForDuties = &SafeValue[bool]{}
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = eligibleShares()
	)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*32-3, testSlotDuration, testEpochsPerSCPeriod)
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-3))
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	waitForDuties.Set(true)
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 3))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     testEpochsPerSCPeriod*32 - 3,
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent()(e.Data.(*v1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: Ticker with no action
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-2))
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 2))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     testEpochsPerSCPeriod*32 - 2,
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})
	scheduler.HandleHeadEvent()(e.Data.(*v1.HeadEvent))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 3: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	duties, _ := dutiesMap.Get(1)
	dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 5},
		ValidatorIndex: phase0.ValidatorIndex(3),
	}))
	waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

	// STEP 5: wait for sync committee duties to be fetched again for the current epoch
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32-1))
	ticker.Send(phase0.Slot(testEpochsPerSCPeriod*32 - 1))
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetch(t, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed, but the second and the new from indices change should
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(testEpochsPerSCPeriod*32))
	duties, _ = dutiesMap.Get(1)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*32)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(testEpochsPerSCPeriod * 32))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_SyncCommittee_Early_Block(t *testing.T) {
	t.Parallel()

	var (
		handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties())
		waitForDuties = &SafeValue[bool]{}
		dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
		activeShares  = eligibleShares()
	)
	dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	ctx, cancel := context.WithCancel(t.Context())
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, 0, testSlotDuration, testEpochsPerSCPeriod)
	fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
	startScheduler(ctx, t, scheduler, schedulerPool)

	duties, _ := dutiesMap.Get(0)
	expected := expectedExecutedSyncCommitteeDuties(handler, duties, 0)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
	ticker.Send(phase0.Slot(0))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 2: expect sync committee duties to be executed at the same period
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(1))
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, 1)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

	ticker.Send(phase0.Slot(1))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

	// STEP 3: wait for sync committee duties to be executed faster than 1/3 of the slot duration when
	// Beacon head event is observed (block arrival)
	waitForSlotN(scheduler.beaconConfig, phase0.Slot(2))
	startTime := time.Now()
	duties, _ = dutiesMap.Get(0)
	expected = expectedExecutedSyncCommitteeDuties(handler, duties, 2)
	setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))
	ticker.Send(phase0.Slot(2))

	// STEP 4: trigger head event (block arrival)
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot: 2,
		},
	}
	scheduler.HandleHeadEvent()(e.Data.(*v1.HeadEvent))
	waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)
	assertWaitedOneThird(t, scheduler.beaconConfig, startTime)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func eligibleShares() []*ssvtypes.SSVShare {
	var result []*ssvtypes.SSVShare

	participationShares := []*ssvtypes.SSVShare{
		{
			// share that is participating
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
			Status:     v1.ValidatorStateActiveOngoing,
			Liquidated: false,
		},
		{
			// share that is participating
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 2,
			},
			Status:     v1.ValidatorStateActiveExiting,
			Liquidated: false,
		},
		{
			// share that is participating
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 3,
			},
			Status:          v1.ValidatorStatePendingQueued,
			Liquidated:      false,
			ActivationEpoch: 0,
		},
	}

	exitingShares := []*ssvtypes.SSVShare{
		{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 4,
			},
			Status: v1.ValidatorStateExitedUnslashed,
		},
		{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 5,
			},
			Status: v1.ValidatorStateExitedSlashed,
		},
		{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 6,
			},
			Status: v1.ValidatorStateWithdrawalDone,
		},
		{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 7,
			},
			Status: v1.ValidatorStateWithdrawalPossible,
		},
	}

	result = append(result, participationShares...)
	result = append(result, exitingShares...)

	return result
}
