package duties

import (
	"bytes"
	"context"
	"testing"
	"testing/synctest"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
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

func setupCommitteeDutiesMock(
	s *Scheduler,
	activeShares []*ssvtypes.SSVShare,
	attDuties *hashmap.Map[phase0.Epoch, []*eth2apiv1.AttesterDuty],
	syncDuties *hashmap.Map[uint64, []*eth2apiv1.SyncCommitteeDuty],
	waitForDuties *SafeValue[bool],
) (chan struct{}, chan committeeDutiesMap) {
	// fetchDutiesCall relays/signals duty-fetch calls, it is buffered so that our test code can run in a single
	// go-routine (so that we don't need to worry about draining this channel to let the execution proceed). The
	// buffer size should be large enough for the test to not block.
	fetchDutiesCall := make(chan struct{}, 100)
	// executeDutiesCall is similar to fetchDutiesCall but signals the duty-executions.
	executeDutiesCall := make(chan committeeDutiesMap, 100)

	s.beaconNode.(*MockBeaconNode).EXPECT().AttesterDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			duties, _ := attDuties.Get(epoch)
			return duties, nil
		}).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			period := s.netCfg.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			duties, _ := syncDuties.Get(period)
			return duties, nil
		}).AnyTimes()

	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfParticipatingValidators(gomock.Any()).Return(activeShares).AnyTimes()
	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfValidators().Return(activeShares).MinTimes(1)
	s.validatorProvider.(*MockValidatorProvider).EXPECT().Validator(gomock.Any()).DoAndReturn(
		func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
			var ssvShare *ssvtypes.SSVShare
			var minEpoch phase0.Epoch
			attDuties.Range(func(epoch phase0.Epoch, duties []*eth2apiv1.AttesterDuty) bool {
				for _, duty := range duties {
					if bytes.Equal(duty.PubKey[:], pubKey) {
						ssvShare = &ssvtypes.SSVShare{
							Share: spectypes.Share{
								ValidatorIndex: duty.ValidatorIndex,
							},
							Status: eth2apiv1.ValidatorStateActiveOngoing,
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

			if ssvShare == nil {
				minEpoch = phase0.Epoch(0)
				syncDuties.Range(func(period uint64, duties []*eth2apiv1.SyncCommitteeDuty) bool {
					for _, duty := range duties {
						if bytes.Equal(duty.PubKey[:], pubKey) {
							ssvShare = &ssvtypes.SSVShare{
								Share: spectypes.Share{
									ValidatorIndex: duty.ValidatorIndex,
								},
								Status: eth2apiv1.ValidatorStateActiveOngoing,
							}
							firstEpoch := s.netCfg.Beacon.FirstEpochOfSyncPeriod(period)
							if firstEpoch < minEpoch {
								minEpoch = firstEpoch
								ssvShare.SetMinParticipationEpoch(firstEpoch)
							}
							return true
						}
					}
					return true
				})
			}

			if ssvShare != nil {
				return ssvShare, true
			}

			return nil, false
		},
	).AnyTimes()

	s.validatorController.(*MockValidatorController).EXPECT().FilterIndices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(afterInit bool, filter func(*ssvtypes.SSVShare) bool) []phase0.ValidatorIndex {
			return indicesFromShares(activeShares)
		}).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitBeaconCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitSyncCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func TestScheduler_Committee_Same_Slot_Attester_Only(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore    = dutystore.New()
			attHandler   = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler  = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler  = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			attDuties    = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties   = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares = []*ssvtypes.SSVShare{activeShare(1)}
		)
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		waitForSlotN(scheduler.netCfg.Beacon, 1)
		startTime := time.Now()
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for attester duties to be fetched and executed at the same slot
		duties, _ := attDuties.Get(phase0.Epoch(0))
		committeeMap := commHandler.buildCommitteeDuties(duties, nil, 0, 1)

		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(1))

		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		assertWaitedOneThird(t, scheduler.netCfg.Beacon, startTime)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Committee_Same_Slot_SyncCommittee_Only(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore    = dutystore.New()
			attHandler   = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler  = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler  = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			attDuties    = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties   = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares = []*ssvtypes.SSVShare{activeShare(1)}
		)
		syncDuties.Set(0, []*eth2apiv1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		waitForSlotN(scheduler.netCfg.Beacon, 1)
		startTime := time.Now()
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for attester duties to be fetched and executed at the same slot
		duties, _ := syncDuties.Get(0)
		committeeMap := commHandler.buildCommitteeDuties(nil, duties, 0, 1)

		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(1))

		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		assertWaitedOneThird(t, scheduler.netCfg.Beacon, startTime)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Committee_Same_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore    = dutystore.New()
			attHandler   = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler  = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler  = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			attDuties    = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties   = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares = []*ssvtypes.SSVShare{activeShare(1)}
		)
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		syncDuties.Set(0, []*eth2apiv1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		waitForSlotN(scheduler.netCfg.Beacon, 1)
		startTime := time.Now()
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for attester duties to be fetched and executed at the same slot
		aDuties, _ := attDuties.Get(0)
		sDuties, _ := syncDuties.Get(0)
		committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, 1)

		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(1))

		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		assertWaitedOneThird(t, scheduler.netCfg.Beacon, startTime)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Committee_Diff_Slot_Attester_Only(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore    = dutystore.New()
			attHandler   = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler  = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler  = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			attDuties    = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties   = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares = []*ssvtypes.SSVShare{activeShare(1)}
		)
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for attester duties to be fetched using handle initial duties
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		// STEP 2: wait for no action to be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for committee duties to be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		startTime := time.Now()
		aDuties, _ := attDuties.Get(0)
		sDuties, _ := syncDuties.Get(0)
		committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, 2)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(2))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		assertWaitedOneThird(t, scheduler.netCfg.Beacon, startTime)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Committee_Indices_Changed_Attester_Only(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore     = dutystore.New()
			attHandler    = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			waitForDuties = &SafeValue[bool]{}
			attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares  = []*ssvtypes.SSVShare{activeShare(1), activeShare(2), activeShare(3)}
		)

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken
		ticker.Send(phase0.Slot(0))
		// no execution should happen in slot 0
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: trigger a change in active indices & wait for attester duties to be re-fetched in slot 0
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(0),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
			{
				PubKey:         phase0.BLSPubKey{1, 2, 5},
				Slot:           phase0.Slot(2),
				ValidatorIndex: phase0.ValidatorIndex(3),
			},
		})
		scheduler.indicesChgCh <- struct{}{}
		waitForDuties.Set(true)
		// Two fetches happen here: one for the attester handler and one for the sync-committee handler.
		// The sync-committee handler's indicesChangeCh is now consumed inside the slot-tick closure
		// (not the outer select), so it also triggers a re-fetch on the same tick as the attester.
		// The test name says "Attester_Only" because no sync-committee duties are assigned — the
		// sync-committee handler still re-fetches (period intent reset), but finds no duties.
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		// no other fetching or execution should happen in slot 0
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for no action to be taken in slot 1
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: wait for committee duties to be executed in slot 2
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		startTime := time.Now()
		aDuties, _ := attDuties.Get(0)
		committeeMap := commHandler.buildCommitteeDuties([]*eth2apiv1.AttesterDuty{aDuties[1]}, nil, 0, 2)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(2))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		assertWaitedOneThird(t, scheduler.netCfg.Beacon, startTime)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Committee_Indices_Changed_Attester_Only_2(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore     = dutystore.New()
			attHandler    = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			waitForDuties = &SafeValue[bool]{}
			attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares  = []*ssvtypes.SSVShare{activeShare(1), activeShare(2), activeShare(3)}
		)

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken
		ticker.Send(phase0.Slot(0))
		// no execution should happen in slot 0
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: trigger a change in active indices & wait for attester duties to be re-fetched in slot 0
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(0),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
			{
				PubKey:         phase0.BLSPubKey{1, 2, 4},
				Slot:           phase0.Slot(2),
				ValidatorIndex: phase0.ValidatorIndex(2),
			},
			{
				PubKey:         phase0.BLSPubKey{1, 2, 5},
				Slot:           phase0.Slot(2),
				ValidatorIndex: phase0.ValidatorIndex(3),
			},
		})
		scheduler.indicesChgCh <- struct{}{}
		waitForDuties.Set(true)
		// wait for attester and sync committee duties to be fetched in slot 0
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		// no other fetching or execution should happen in slot 0
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for no action to be taken in slot 1
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: wait for committee duties to be executed in slot 2
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		startTime := time.Now()
		aDuties, _ := attDuties.Get(0)
		committeeMap := commHandler.buildCommitteeDuties([]*eth2apiv1.AttesterDuty{aDuties[1], aDuties[2]}, nil, 0, 2)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(2))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		assertWaitedOneThird(t, scheduler.netCfg.Beacon, startTime)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Committee_Indices_Changed_Attester_Only_3(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore     = dutystore.New()
			attHandler    = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			waitForDuties = &SafeValue[bool]{}
			attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares  = []*ssvtypes.SSVShare{activeShare(1), activeShare(2)}
		)
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for attester duties to be fetched using handle initial duties
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken
		ticker.Send(phase0.Slot(0))
		// no execution should happen in slot 0
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: trigger a change in active indices & wait for attester duties to be re-fetched in slot 0
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 5},
				Slot:           phase0.Slot(2),
				ValidatorIndex: phase0.ValidatorIndex(2),
			},
		})
		scheduler.indicesChgCh <- struct{}{}
		waitForDuties.Set(true)
		// wait for attester and sync committee duties to be fetched in slot 0
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		// no other fetching or execution should happen in slot 0
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for no action to be taken in slot 1
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: wait for committee duties to be executed in slot 2
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		startTime := time.Now()
		aDuties, _ := attDuties.Get(0)
		committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, 2)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(2))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		assertWaitedOneThird(t, scheduler.netCfg.Beacon, startTime)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed
func TestScheduler_Committee_Reorg_Previous_Epoch_Transition_Attester_only(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore     = dutystore.New()
			attHandler    = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			waitForDuties = &SafeValue[bool]{}
			attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
		)

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler}, testSlotsPerEpoch*2-1)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch*2-1)
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)

		attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for attester duties to be fetched for the next epoch only (since we start at
		// the last slot of the epoch), plus for sync committee duties to be fetched for the current period.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout) // (attester) next epoch fetch-call
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout) // (sync committee) current epoch fetch-call

		// STEP 2: trigger head event
		e := &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch*2 - 1,
				CurrentDutyDependentRoot:  phase0.Root{0x01},
				PreviousDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: Ticker with no action
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on epoch transition
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch * 2,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		// wait for attester duties to be re-fetched for the current epoch
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		// no execution should happen in slot testSlotsPerEpoch*2
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: execute reorged duty
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+1))
		aDuties, _ := attDuties.Get(phase0.Epoch(2))
		committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, testSlotsPerEpoch*2+1)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		// STEP 6: The first assigned duty should not be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 2))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed and the indices changed as well
func TestScheduler_Committee_Reorg_Previous_Epoch_Transition_Indices_Changed_Attester_only(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore     = dutystore.New()
			attHandler    = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			waitForDuties = &SafeValue[bool]{}
			attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares  = []*ssvtypes.SSVShare{activeShare(1), activeShare(2)}
		)

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler}, testSlotsPerEpoch*2-1)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch*2-1)
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)

		attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for attester duties to be fetched for the next epoch only (since we start at
		// the last slot of the epoch), plus for sync committee duties to be fetched for the current period.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout) // (attester) next epoch fetch-call
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout) // (sync committee) current epoch fetch-call

		// STEP 2: trigger head event
		e := &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch*2 - 1,
				CurrentDutyDependentRoot:  phase0.Root{0x01},
				PreviousDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: Ticker with no action
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on epoch transition
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch * 2,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		// wait for attester duties to be fetched
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		// no execution should happen in slot testSlotsPerEpoch*2
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: trigger indices change
		scheduler.indicesChgCh <- struct{}{}
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		attDuties.Delete(phase0.Epoch(2))

		// STEP 6: wait for attester duties to be re-fetched for the current epoch
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		// Wait for the slot ticker to be triggered in the attester, sync committee, and cluster handlers.
		// This ensures that no attester duties are fetched before the cluster ticker is triggered,
		// preventing a scenario where the cluster handler executes duties in the same slot as the attester fetching them.
		time.Sleep(testSlotTickerTriggerDelay)

		// wait for attester duties to be fetched
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		// wait for sync committee duties to be fetched
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)

		// STEP 7: The first assigned duty should not be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 2))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The reorg assigned duty should not be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+3))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 3))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed
func TestScheduler_Committee_Reorg_Previous_Attester_only(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore     = dutystore.New()
			attHandler    = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			waitForDuties = &SafeValue[bool]{}
			attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
		)

		attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for attester duties to be fetched using handle initial duties
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler}, testSlotsPerEpoch)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch)
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		waitForDuties.Set(true)
		// STEP 2: trigger head event
		e := &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch,
				PreviousDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: Ticker with no action
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 1))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch + 1,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 4),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		// wait for attester duties to be re-fetched for the current epoch
		waitForDutiesFetchCommittee(t, fetchDutiesCall, executeDutiesCall, timeout)
		// no execution should happen in slot testSlotsPerEpoch+1
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: Ticker with no action
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 2))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: The first assigned duty should not be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+3))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 3))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 7: execute reorged duty
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+4))
		aDuties, _ := attDuties.Get(phase0.Epoch(1))
		committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, testSlotsPerEpoch+4)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(testSlotsPerEpoch + 4))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Committee_Early_Block_Attester_Only(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore    = dutystore.New()
			attHandler   = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler  = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler  = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			attDuties    = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties   = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares = []*ssvtypes.SSVShare{activeShare(1)}
		)
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
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
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		ticker.Send(phase0.Slot(0))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for no action to be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration when
		// Beacon head event is observed (block arrival)
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		aDuties, _ := attDuties.Get(0)
		committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, 2)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))
		startTime := time.Now()
		ticker.Send(phase0.Slot(2))

		// STEP 4: trigger head event (block arrival)
		e := &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot: 2,
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)
		require.Less(t, time.Since(startTime), scheduler.netCfg.SlotDuration/3)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Committee_Early_Block(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore    = dutystore.New()
			attHandler   = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler  = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler  = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			attDuties    = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties   = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares = []*ssvtypes.SSVShare{activeShare(1)}
		)
		attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		syncDuties.Set(0, []*eth2apiv1.SyncCommitteeDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		// STEP 1: wait for attester & sync committee duties to be fetched (handle initial duties)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler})
		waitForSlotN(scheduler.netCfg.Beacon, 1)
		startTime := time.Now()
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		// STEP 2: wait for committee duty to be executed
		aDuties, _ := attDuties.Get(0)
		sDuties, _ := syncDuties.Get(0)
		committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, 1)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(1))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		assertWaitedOneThird(t, scheduler.netCfg.Beacon, startTime)

		// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration when
		// Beacon head event is observed (block arrival)
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		committeeMap = commHandler.buildCommitteeDuties(nil, sDuties, 0, 2)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))
		startTime = time.Now()
		ticker.Send(phase0.Slot(2))

		// STEP 4: trigger head event (block arrival)
		e := &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot: 2,
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)
		require.Less(t, time.Since(startTime), scheduler.netCfg.SlotDuration/3)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestCommitteeHandlerShouldExecuteSyncIgnoresMinParticipationEpoch(t *testing.T) {
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

	handler := NewCommitteeHandler(dutystore.New().Attester, dutystore.New().SyncCommittee)
	handler.logger = zap.NewNop()
	handler.netCfg = &netCfg
	handler.validatorProvider = validatorProvider

	shouldExecute := handler.shouldExecuteSync(&eth2apiv1.SyncCommitteeDuty{
		PubKey:         phase0.BLSPubKey(share.ValidatorPubKey),
		ValidatorIndex: share.ValidatorIndex,
	}, 0, 0)

	require.True(t, shouldExecute)
}

func TestCommitteeHandlerBuildCommitteeDutiesIncludesSyncButNotAttesterDuringParticipationDelay(t *testing.T) {
	ctrl := gomock.NewController(t)

	share := activeShare(1)
	share.ValidatorPubKey = spectypes.ValidatorPK{1, 2, 3}
	share.SetMinParticipationEpoch(1)

	validatorProvider := NewMockValidatorProvider(ctrl)
	validatorProvider.EXPECT().SelfParticipatingValidators(phase0.Epoch(0)).Return([]*ssvtypes.SSVShare{share})
	validatorProvider.EXPECT().Validator(share.ValidatorPubKey[:]).Return(share, true).Times(2)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.GenesisTime = time.Now()
	beaconCfg.SlotDuration = time.Hour
	beaconCfg.SlotsPerEpoch = testSlotsPerEpoch
	netCfg := *networkconfig.TestNetwork
	netCfg.Beacon = &beaconCfg

	dutyStore := dutystore.New()
	handler := NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
	handler.logger = zap.NewNop()
	handler.netCfg = &netCfg
	handler.validatorProvider = validatorProvider

	committeeMap := handler.buildCommitteeDuties(
		[]*eth2apiv1.AttesterDuty{{
			PubKey:         phase0.BLSPubKey(share.ValidatorPubKey),
			Slot:           0,
			ValidatorIndex: share.ValidatorIndex,
		}},
		[]*eth2apiv1.SyncCommitteeDuty{{
			PubKey:         phase0.BLSPubKey(share.ValidatorPubKey),
			ValidatorIndex: share.ValidatorIndex,
		}},
		0,
		0,
	)

	require.Len(t, committeeMap, 1)
	committeeDuty := committeeMap[share.CommitteeID()]
	require.NotNil(t, committeeDuty)
	validatorDuties := committeeDuty.validatorDuties()
	require.Len(t, validatorDuties, 1)
	require.Equal(t, spectypes.BNRoleSyncCommittee, validatorDuties[0].Type)
	require.Equal(t, share.ValidatorIndex, validatorDuties[0].ValidatorIndex)
}

// The purpose of the test is to ensure that the scheduler can handle the case where the indices change
// at the last slot of the epoch, and it does not affect the execution of the duties for the next epoch first slot.
func TestScheduler_Committee_Indices_Changed_At_The_Last_Slot_Of_The_Epoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			dutyStore    = dutystore.New()
			attHandler   = NewAttesterHandler(dutyStore.Attester, false)
			syncHandler  = NewSyncCommitteeHandler(dutyStore.SyncCommittee, false)
			commHandler  = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
			attDuties    = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			syncDuties   = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
			activeShares = []*ssvtypes.SSVShare{
				activeShare(1),
				activeShare(2),
				activeShare(3),
			}
		)
		attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
			{
				PubKey:         phase0.BLSPubKey{1, 2, 4},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(2),
			},
		})

		// STEP 1: wait for attester duties to be fetched using handle initial duties
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{attHandler, syncHandler, commHandler}, testSlotsPerEpoch-1)
		fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken
		ticker.Send(phase0.Slot(0))
		// no execution should happen in slot 0
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for no action to be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch-1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch - 1))
		// no execution should happen in slot testSlotsPerEpoch-1
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: trigger a change in active indices at the last slot of the epoch
		scheduler.indicesChgCh <- struct{}{}
		waitForNoActionCommittee(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: the first slot of the next epoch duties should be executed as expected
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch))

		aDuties, _ := attDuties.Get(1)
		committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 1, testSlotsPerEpoch)
		setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

		ticker.Send(phase0.Slot(testSlotsPerEpoch))
		waitForDutiesExecutionCommittee(t, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func activeShare(index phase0.ValidatorIndex) *ssvtypes.SSVShare {
	return &ssvtypes.SSVShare{
		Share: spectypes.Share{
			Committee: []*spectypes.ShareMember{
				{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
			},
			ValidatorIndex: index,
		},
		Status: eth2apiv1.ValidatorStateActiveOngoing,
	}
}

func assertWaitedOneThird(t *testing.T, beaconConfig *networkconfig.Beacon, startTime time.Time) {
	// validate the 1/3 of the slot waiting time
	require.Less(t, beaconConfig.SlotDuration/3, time.Since(startTime)+clockError)
}
