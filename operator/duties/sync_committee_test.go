package duties

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func TestSyncCommitteeHandlerShouldExecuteIgnoresMinParticipationEpoch(t *testing.T) {
	ctrl := gomock.NewController(t)

	share := activeShare(1)
	share.ValidatorPubKey = spectypes.ValidatorPK{1, 2, 3}
	share.SetMinParticipationEpoch(1)

	validatorProvider := NewMockValidatorProvider(ctrl)
	validatorProvider.EXPECT().Validator(share.ValidatorPubKey[:]).Return(share, true)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.GenesisTime = time.Now()
	beaconCfg.SlotDuration = time.Hour
	beaconCfg.SlotsPerEpoch = testSlotsPerEpoch
	netCfg := *networkconfig.TestNetwork
	netCfg.Beacon = &beaconCfg

	handler := NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
	handler.logger = zap.NewNop()
	handler.netCfg = &netCfg
	handler.validatorProvider = validatorProvider

	shouldExecute := handler.shouldExecute(&v1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey(share.ValidatorPubKey),
		ValidatorIndex: share.ValidatorIndex,
	}, 0)

	require.True(t, shouldExecute)
}

func setupSyncCommitteeDutiesMock(
	s *Scheduler,
	activeShares []*ssvtypes.SSVShare,
	dutiesMap *hashmap.Map[uint64, []*v1.SyncCommitteeDuty],
	waitForDuties *SafeValue[bool],
) (chan struct{}, chan []*spectypes.ValidatorDuty) {
	return setupSyncCommitteeDutiesMockWithFetcher(s, activeShares, dutiesMap, waitForDuties, nil)
}

func setupSyncCommitteeDutiesMockWithFetcher(
	s *Scheduler,
	activeShares []*ssvtypes.SSVShare,
	dutiesMap *hashmap.Map[uint64, []*v1.SyncCommitteeDuty],
	waitForDuties *SafeValue[bool],
	fetcher func(context.Context, phase0.Epoch, []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error),
) (chan struct{}, chan []*spectypes.ValidatorDuty) {
	// fetchDutiesCall relays/signals duty-fetch calls, it is buffered so that our test code can run in a single
	// go-routine (so that we don't need to worry about draining this channel to let the execution proceed). The
	// buffer size of 2 covers the current and the next periods.
	fetchDutiesCall := make(chan struct{}, 100)
	// executeDutiesCall is similar to fetchDutiesCall but signals the duty-executions.
	executeDutiesCall := make(chan []*spectypes.ValidatorDuty, 100)

	if fetcher == nil {
		fetcher = func(_ context.Context, epoch phase0.Epoch, _ []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
			period := s.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			duties, _ := dutiesMap.Get(period)
			return duties, nil
		}
	}

	s.beaconNode.(*MockBeaconNode).EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			return fetcher(ctx, epoch, indices)
		}).AnyTimes()

	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfParticipatingValidators(gomock.Any()).Return(activeShares).AnyTimes()
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
						firstEpoch := s.netCfg.FirstEpochOfSyncPeriod(period)
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
	expectedDuties := make([]*spectypes.ValidatorDuty, 0, len(duties))
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommitteeContribution))
	}
	return expectedDuties
}

func waitForFetchedSyncCommitteePeriod(
	t *testing.T,
	fetchDutiesCall chan struct{},
	fetchedPeriods chan uint64,
	timeout time.Duration,
	expectedPeriod uint64,
) {
	waitForDutiesFetch(t, fetchDutiesCall, timeout)

	select {
	case period := <-fetchedPeriods:
		require.Equal(t, expectedPeriod, period)
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for fetched sync committee period")
	}
}

func TestScheduler_SyncCommittee_Same_Period(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler      = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap    = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			activeShares = eligibleShares()
		)
		dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for sync committee duties to be fetched (handle initial duties)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		firstSlotOfNextPeriod := phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch)
		lastSlotOfPeriod := firstSlotOfNextPeriod - 2
		startSlot := lastSlotOfPeriod - 2
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
		waitForSlotN(scheduler.netCfg.Beacon, startSlot)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
		duties, _ := dutiesMap.Get(0)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, startSlot)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(startSlot)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 2: expect sync committee duties to be executed at the same period
		waitForSlotN(scheduler.netCfg.Beacon, startSlot+1)
		duties, _ = dutiesMap.Get(0)
		expected = expectedExecutedSyncCommitteeDuties(handler, duties, startSlot+1)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(startSlot + 1)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 3: expect sync committee duties to be executed at the last slot of the period
		waitForSlotN(scheduler.netCfg.Beacon, lastSlotOfPeriod)
		duties, _ = dutiesMap.Get(0)
		expected = expectedExecutedSyncCommitteeDuties(handler, duties, lastSlotOfPeriod)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(lastSlotOfPeriod)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 4: expect no action to be taken as we are in the next period
		waitForSlotN(scheduler.netCfg.Beacon, firstSlotOfNextPeriod)
		ticker.Send(firstSlotOfNextPeriod)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Current_Next_Periods(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler        = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
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
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		firstSlotOfNextPeriod := phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch)
		lastSlotOfPeriod := firstSlotOfNextPeriod - 2
		startSlot := lastSlotOfPeriod - 2
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
		waitForSlotN(scheduler.netCfg.Beacon, startSlot)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, eligibleShares, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		duties, _ := dutiesMap.Get(0)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, startSlot)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(startSlot)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 2: wait for sync committee duties to be executed
		waitForSlotN(scheduler.netCfg.Beacon, startSlot+1)
		duties, _ = dutiesMap.Get(0)
		expected = expectedExecutedSyncCommitteeDuties(handler, duties, startSlot+1)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(startSlot + 1)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 3: wait for sync committee duties to be executed
		waitForSlotN(scheduler.netCfg.Beacon, lastSlotOfPeriod)
		duties, _ = dutiesMap.Get(0)
		expected = expectedExecutedSyncCommitteeDuties(handler, duties, lastSlotOfPeriod)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(lastSlotOfPeriod)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// ...

		// STEP 4: new period, wait for sync committee duties to be executed
		waitForSlotN(scheduler.netCfg.Beacon, firstSlotOfNextPeriod)
		duties, _ = dutiesMap.Get(1)
		expected = expectedExecutedSyncCommitteeDuties(handler, duties, firstSlotOfNextPeriod)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(firstSlotOfNextPeriod)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			waitForDuties = &SafeValue[bool]{}
			dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			activeShares  = eligibleShares()
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*testSlotsPerEpoch-3)
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-3))
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)

		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for sync committee duties to be fetched for the current period only
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 2: trigger a change in active indices
		scheduler.indicesChgCh <- struct{}{}
		duties, _ := dutiesMap.Get(1)
		dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for sync committee duties to be re-fetched twice:
		// once for the regular next-period prefetch and once while handling the indices change.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-2))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: no action should be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-1))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: execute duties
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		duties, _ = dutiesMap.Get(1)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*testSlotsPerEpoch)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			waitForDuties = &SafeValue[bool]{}
			dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			activeShares  = eligibleShares()
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*testSlotsPerEpoch-3)
		waitForSlotN(scheduler.netCfg.Beacon, testEpochsPerSCPeriod*testSlotsPerEpoch-3)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 3))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: trigger a change in active indices
		scheduler.indicesChgCh <- struct{}{}
		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: trigger a change in active indices
		scheduler.indicesChgCh <- struct{}{}
		duties, _ := dutiesMap.Get(1)
		dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: wait for sync committee duties to be re-fetched
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-2))
		waitForDuties.Set(true)
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: no action should be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-1))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: The first assigned duty should not be executed, but the second one should
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		duties, _ = dutiesMap.Get(1)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*testSlotsPerEpoch)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg current dependent root changed
func TestScheduler_SyncCommittee_Reorg_Current(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			waitForDuties = &SafeValue[bool]{}
			dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			activeShares  = eligibleShares()
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*testSlotsPerEpoch-3)
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-3))
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)

		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for sync committee duties to be fetched for the current period only
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 2: trigger head event
		e := &v1.Event{
			Data: &v1.HeadEvent{
				Slot:                     testEpochsPerSCPeriod*testSlotsPerEpoch - 3,
				CurrentDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: ticker pre-fetches duties for the next period
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-2))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg, the current slot re-fetches duties for the next period
		e = &v1.Event{
			Data: &v1.HeadEvent{
				Slot:                     testEpochsPerSCPeriod*testSlotsPerEpoch - 2,
				CurrentDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 4},
				ValidatorIndex: phase0.ValidatorIndex(2),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: wait for no action to be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-1))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: The first assigned duty should not be executed, but the second one should
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		duties, _ := dutiesMap.Get(1)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*testSlotsPerEpoch)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg current dependent root changed including indices change in the same slot
func TestScheduler_SyncCommittee_Reorg_Current_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			waitForDuties = &SafeValue[bool]{}
			dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			activeShares  = eligibleShares()
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testEpochsPerSCPeriod*testSlotsPerEpoch-3)
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-3))
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)

		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for sync committee duties to be fetched for the current period only
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 2: trigger head event
		e := &v1.Event{
			Data: &v1.HeadEvent{
				Slot:                     testEpochsPerSCPeriod*testSlotsPerEpoch - 3,
				CurrentDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: ticker pre-fetches duties for the next period
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-2))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg, the current slot re-fetches duties for the next period
		e = &v1.Event{
			Data: &v1.HeadEvent{
				Slot:                     testEpochsPerSCPeriod*testSlotsPerEpoch - 2,
				CurrentDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 4},
				ValidatorIndex: phase0.ValidatorIndex(2),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: trigger a change in active indices in the same slot
		scheduler.indicesChgCh <- struct{}{}
		duties, _ := dutiesMap.Get(1)
		dutiesMap.Set(1, append(duties, &v1.SyncCommitteeDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 5},
			ValidatorIndex: phase0.ValidatorIndex(3),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: the boundary tick re-fetches duties for the next period due to the indices change.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-1))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 7: The first assigned duty should not be executed, but the second and the new from indices change should.
		// With the period-boundary check applying on slot 46+, the indices change at slot 47 already refreshed p1,
		// so entering the new period executes without an additional fetch.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		duties, _ = dutiesMap.Get(1)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*testSlotsPerEpoch)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Early_Block(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler      = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap    = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			activeShares = eligibleShares()
		)
		dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		duties, _ := dutiesMap.Get(0)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, 0)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		// STEP 1: wait for sync committee duties to be fetched and executed at the same slot
		ticker.Send(phase0.Slot(0))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 2: expect sync committee duties to be executed at the same period
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		duties, _ = dutiesMap.Get(0)
		expected = expectedExecutedSyncCommitteeDuties(handler, duties, 1)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 3: wait for sync committee duties to be executed faster than 1/3 of the slot duration when
		// Beacon head event is observed (block arrival)
		startTime := time.Now()
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
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
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)
		require.Greater(t, time.Since(startTime), time.Duration(float64(scheduler.netCfg.SlotDuration/3)*0.90))

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Indices_Changed_Too_Late_In_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			waitForDuties = &SafeValue[bool]{}
			activeShares  = eligibleShares()
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: slot 0 has no duties and no action.
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: arrange for indices change to arrive too late for slot 0 processing.
		waitForDuties.Set(true)
		dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		go func() {
			time.Sleep(scheduler.netCfg.IntervalDuration(0) + 1*time.Millisecond)
			scheduler.indicesChgCh <- struct{}{}
		}()

		// No fetching should happen on slot 0 because the indices change arrived too late in the slot.
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: on slot 1 the deferred indices change is processed and duties are fetched.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: on slot 2 the fetched duty executes.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		duties, _ := dutiesMap.Get(0)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, phase0.Slot(2))
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(2))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Reorg_Previous_Epoch_Transition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			waitForDuties = &SafeValue[bool]{}
			activeShares  = eligibleShares()
		)

		initialPeriod1Duties := []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		}
		dutiesMap.Set(1, initialPeriod1Duties)

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		startSlot := phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 2)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
		waitForSlotN(scheduler.netCfg.Beacon, startSlot)
		fetchedPeriods := make(chan uint64, 100)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMockWithFetcher(
			scheduler,
			activeShares,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
				period := scheduler.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
				fetchedPeriods <- period
				duties, _ := dutiesMap.Get(period)
				return duties, nil
			},
		)

		// STEP 1: startup at the last slot fetches duties for the next period.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)

		// STEP 2: establish the baseline head state for the current period.
		e := &v1.Event{
			Data: &v1.HeadEvent{
				Slot:                      startSlot,
				Block:                     phase0.Root{0x03},
				CurrentDutyDependentRoot:  phase0.Root{0x02},
				PreviousDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: slot 47 has no action because the period duties were already prefetched.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-1))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: previous-duty-dependent-root changes on the period transition.
		// Sync committee duties should ignore it and keep using the already fetched current-period duties.
		dutiesMap.Set(1, append(initialPeriod1Duties, &v1.SyncCommitteeDuty{
			PubKey:         phase0.BLSPubKey{4, 5, 6},
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		e = &v1.Event{
			Data: &v1.HeadEvent{
				Slot:                      phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch),
				Block:                     phase0.Root{0x05},
				CurrentDutyDependentRoot:  phase0.Root{0x02},
				PreviousDutyDependentRoot: phase0.Root{0x04},
			},
		}
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: enter the new period. The originally fetched duties still execute, proving there was no refetch.
		expected := expectedExecutedSyncCommitteeDuties(handler, initialPeriod1Duties, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Retry_Current_Period_Fetch_On_Next_Tick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler              = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap            = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			waitForDuties        = &SafeValue[bool]{}
			currentPeriodFetches atomic.Int32
			activeShares         = eligibleShares()
		)
		dutiesMap.Set(0, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMockWithFetcher(
			scheduler,
			activeShares,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
				period := scheduler.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
				duties, _ := dutiesMap.Get(period)
				if period == 0 && currentPeriodFetches.Add(1) <= 3 {
					return nil, errors.New("failed to fetch sync committee duties")
				}
				return duties, nil
			},
		)

		// STEP 1: fail the initial current-period fetch.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 2: fail the retry on slot 0.
		ticker.Send(phase0.Slot(0))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: fail the retry on slot 1.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: retry again on slot 2, succeed, and execute duties in the same slot.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		duties, _ := dutiesMap.Get(0)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, phase0.Slot(2))
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_No_Eligible_Validators_Leaves_Current_Period_Fetch_Pending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap     = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, nil, dutiesMap, waitForDuties)

		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))

		// With no eligible validators, the startup fetch is a no-op that must NOT mark the intent fulfilled —
		// otherwise the duty would never be fetched once a validator becomes eligible (e.g. after a metadata
		// sync that lands without an accompanying indices-change event). The intent stays pending.
		require.False(t, handler.dutyFetchIntents[0])
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// The next tick re-evaluates the pending intent. There are still no eligible validators, so it
		// short-circuits before any fetch and the intent remains pending (ready to be retried later).
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		require.False(t, handler.dutyFetchIntents[0])

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Retry_Next_Period_Fetch_On_Next_Tick_Mid_Period(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler           = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap         = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			waitForDuties     = &SafeValue[bool]{}
			nextPeriodFetches atomic.Int32
			activeShares      = eligibleShares()
		)
		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, phase0.Slot(29))
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(29))
		fetchedPeriods := make(chan uint64, 100)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMockWithFetcher(
			scheduler,
			activeShares,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
				period := scheduler.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
				fetchedPeriods <- period
				duties, _ := dutiesMap.Get(period)
				if period == 1 && nextPeriodFetches.Add(1) <= 3 {
					return nil, errors.New("failed to fetch sync committee duties")
				}
				return duties, nil
			},
		)

		// STEP 1: on startup, fetch duties for the current period successfully.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 0)

		// STEP 2: on the first tick, fail to fetch duties for the next period.
		ticker.Send(phase0.Slot(29))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(30))
		ticker.Send(phase0.Slot(30))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(31))
		ticker.Send(phase0.Slot(31))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: retry on the next tick and succeed.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(32))
		ticker.Send(phase0.Slot(32))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Retry_Next_Period_Fetch_On_Next_Tick_Period_Transition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler           = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap         = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			waitForDuties     = &SafeValue[bool]{}
			nextPeriodFetches atomic.Int32
			activeShares      = eligibleShares()
		)
		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		startSlot := phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 3)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
		waitForSlotN(scheduler.netCfg.Beacon, startSlot)
		fetchedPeriods := make(chan uint64, 100)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMockWithFetcher(
			scheduler,
			activeShares,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
				period := scheduler.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
				fetchedPeriods <- period
				duties, _ := dutiesMap.Get(period)
				if period == 1 && nextPeriodFetches.Add(1) <= 3 {
					return nil, errors.New("failed to fetch sync committee duties")
				}
				return duties, nil
			},
		)

		// STEP 1: on startup, fetch duties for the current period successfully.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 0)

		// STEP 2: on the first tick, fail to fetch duties for the next period.
		ticker.Send(startSlot)
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, startSlot+1)
		ticker.Send(startSlot + 1)
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, startSlot+2)
		ticker.Send(startSlot + 2)
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: on the next tick, the previously next period becomes current, fetch succeeds, and duties execute.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		duties, _ := dutiesMap.Get(1)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_SyncCommittee_Retry_Next_Period_Fetch_On_Next_Tick_Period_Transition_Start_At_Last_Slot_Of_Current_Period(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler           = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			dutiesMap         = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			waitForDuties     = &SafeValue[bool]{}
			nextPeriodFetches atomic.Int32
			activeShares      = eligibleShares()
		)
		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		startSlot := phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 2)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
		waitForSlotN(scheduler.netCfg.Beacon, startSlot)
		fetchedPeriods := make(chan uint64, 100)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMockWithFetcher(
			scheduler,
			activeShares,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
				period := scheduler.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
				fetchedPeriods <- period
				duties, _ := dutiesMap.Get(period)
				if period == 1 && nextPeriodFetches.Add(1) <= 3 {
					return nil, errors.New("failed to fetch sync committee duties")
				}
				return duties, nil
			},
		)

		// STEP 1: starting at the last slot of the current period fetches the next period immediately, and it fails.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)

		// STEP 2: one more retry happens on slot 47 while the period is still current, and it fails.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch-1))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 1))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: on the period transition tick, retry the previously next period as the now current period and fail.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		ticker.Send(phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: retry on the next tick, succeed, and execute duties in the same slot.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch+1))
		duties, _ := dutiesMap.Get(1)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch+1))
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch + 1))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// A reorg-triggered next-period re-fetch that fails must not drop the duties: the intent stays unfulfilled
// and is retried on later ticks. Regression guard for the reorg path, which is distinct from the ticker path.
func TestScheduler_SyncCommittee_Reorg_Retry_Next_Period_Fetch_On_Next_Tick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler             = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
			waitForDuties       = &SafeValue[bool]{}
			dutiesMap           = hashmap.New[uint64, []*v1.SyncCommitteeDuty]()
			activeShares        = eligibleShares()
			failNextPeriodFetch atomic.Bool
		)
		dutiesMap.Set(1, []*v1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		startSlot := phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch - 3)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, startSlot)
		waitForSlotN(scheduler.netCfg.Beacon, startSlot)
		fetchedPeriods := make(chan uint64, 100)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMockWithFetcher(
			scheduler,
			activeShares,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.SyncCommitteeDuty, error) {
				period := scheduler.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
				fetchedPeriods <- period
				if period == 1 && failNextPeriodFetch.Load() {
					return nil, errors.New("failed to fetch sync committee duties")
				}
				duties, _ := dutiesMap.Get(period)
				return duties, nil
			},
		)

		// STEP 1: on startup, fetch duties for the current period.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 0)

		// STEP 2: record the baseline dependent root via a head event (no reorg detected yet).
		e := &v1.Event{
			Data: &v1.HeadEvent{
				Slot:                     startSlot,
				CurrentDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: the ticker pre-fetches the next-period duties successfully.
		waitForSlotN(scheduler.netCfg.Beacon, startSlot+1)
		ticker.Send(startSlot + 1)
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: a reorg (current dependent root changed) re-fetches the next period, but the fetch fails. The
		// intent must stay unfulfilled (duties are not dropped) rather than being cleared.
		failNextPeriodFetch.Store(true)
		e = &v1.Event{
			Data: &v1.HeadEvent{
				Slot:                     startSlot + 1,
				CurrentDutyDependentRoot: phase0.Root{0x02},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*v1.HeadEvent))
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// The failed re-fetch must not drop the previously-fetched next-period duties - they stay available
		// (a successful re-fetch overwrites them; a failed one leaves them intact). Asserting here, between the
		// failed re-fetch and the STEP 5 retry, is what makes "duties are not dropped" observable: the retry
		// would otherwise repopulate them regardless.
		keptDuties := handler.duties.CommitteePeriodDuties(1)
		require.Len(t, keptDuties, 1, "next-period duties must survive a failed reorg re-fetch")
		require.Equal(t, phase0.ValidatorIndex(1), keptDuties[0].ValidatorIndex)

		// STEP 5: the next tick retries the failed next-period fetch and succeeds.
		failNextPeriodFetch.Store(false)
		waitForSlotN(scheduler.netCfg.Beacon, startSlot+2)
		ticker.Send(startSlot + 2)
		waitForFetchedSyncCommitteePeriod(t, fetchDutiesCall, fetchedPeriods, timeout, 1)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: on the period-transition tick the duties for the (now current) period are executed.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testEpochsPerSCPeriod*testSlotsPerEpoch))
		duties, _ := dutiesMap.Get(1)
		expected := expectedExecutedSyncCommitteeDuties(handler, duties, testEpochsPerSCPeriod*testSlotsPerEpoch)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testEpochsPerSCPeriod * testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// setupSchedulerAndMocksWithBoole creates a scheduler whose Forks.Boole = 0, making
// BooleForkAtSlot true for every slot. This exercises the post-Boole code path where
// the sync-committee handler fetches & stores duties but skips processExecution.
func setupSchedulerAndMocksWithBoole(
	ctx context.Context,
	t *testing.T,
	handlers []dutyHandler,
) (*Scheduler, *mockSlotTickerService) {
	s, ticker := setupSchedulerAndMocksWithParams(ctx, t, handlers, time.Now(), slotDuration)
	// Activate Boole at epoch 0 so BooleForkAtSlot is true for all test slots.
	s.netCfg.SSV.Forks.Boole = 0
	return s, ticker
}

// TestScheduler_SyncCommittee_Boole_Fork_Fetches_But_Does_Not_Execute guards the
// BooleForkAtSlot gate in HandleDuties: post-fork the handler must still FETCH and STORE
// sync-committee duties but must NOT emit BNRoleSyncCommitteeContribution via ExecuteDuty.
//
// Secondary assertion (duties visible to the AggregatorCommittee handler) is skipped —
// wiring that handler into the test harness is a significant lift tracked separately.
func TestScheduler_SyncCommittee_Boole_Fork_Fetches_But_Does_Not_Execute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewSyncCommitteeHandler(dutystore.NewSyncCommitteeDuties(), false)
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

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		// Use the Boole-enabled scheduler: BooleForkAtSlot is always true.
		scheduler, ticker := setupSchedulerAndMocksWithBoole(ctx, t, []dutyHandler{handler})

		// Signal all fetches from the start so we can detect them.
		waitForDuties.Set(true)
		fetchDutiesCall, executeDutiesCall := setupSyncCommitteeDutiesMock(scheduler, activeShares, dutiesMap, waitForDuties)

		// ExecuteDuty is NOT registered on the mock. Any unexpected call will cause gomock to
		// fail the test, confirming that the BooleForkAtSlot gate suppresses Alan execution.
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: HandleInitialDuties fetches & stores period-0 duties.
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		stored := handler.duties.CommitteePeriodDuties(0)
		require.NotEmpty(t, stored, "duties must be stored after HandleInitialDuties")

		// STEP 2: tick slot 0. prepareCurrentPeriod skips the fetch (intent already fulfilled).
		// processExecution is gated by BooleForkAtSlot — it must NOT fire, so no ExecuteDuty call.
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: tick slot 1 — the gate must hold on subsequent ticks too.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// Duties remain stored (fetch side intact, execution side suppressed).
		stored = handler.duties.CommitteePeriodDuties(0)
		require.Len(t, stored, 1, "duties must still be stored after Boole-gated ticks")

		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func eligibleShares() []*ssvtypes.SSVShare {
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

	result := make([]*ssvtypes.SSVShare, 0, len(participationShares)+len(exitingShares))
	result = append(result, participationShares...)
	result = append(result, exitingShares...)

	return result
}
