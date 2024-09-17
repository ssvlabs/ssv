package duties

import (
	"context"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/utils/hashmap"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	mocknetwork "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/mocks"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func setupDutiesMockCommittee(
	s *Scheduler,
	activeShares []*ssvtypes.SSVShare,
	attDuties *hashmap.Map[phase0.Epoch, []*eth2apiv1.AttesterDuty],
	syncDuties *hashmap.Map[uint64, []*eth2apiv1.SyncCommitteeDuty],
) (chan struct{}, chan committeeDutiesMap) {
	fetchDutiesCall := make(chan struct{})
	executeDutiesCall := make(chan committeeDutiesMap)

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

	s.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().GetEpochFirstSlot(gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch) phase0.Slot {
			return phase0.Slot(uint64(epoch) * s.network.Beacon.SlotsPerEpoch())
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

	s.beaconNode.(*MockBeaconNode).EXPECT().AttesterDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
			fetchDutiesCall <- struct{}{}
			duties, _ := attDuties.Get(epoch)
			return duties, nil
		}).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
			fetchDutiesCall <- struct{}{}
			period := s.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			duties, _ := syncDuties.Get(period)
			return duties, nil
		}).AnyTimes()

	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfParticipatingValidators(gomock.Any()).Return(activeShares).AnyTimes()
	s.validatorProvider.(*MockValidatorProvider).EXPECT().ParticipatingValidators(gomock.Any()).Return(activeShares).AnyTimes()

	s.validatorController.(*MockValidatorController).EXPECT().AllActiveIndices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch, afterInit bool) []phase0.ValidatorIndex {
			return indicesFromShares(activeShares)
		}).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitBeaconCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitSyncCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func TestScheduler_Committee_Same_Slot_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	duties, _ := attDuties.Get(phase0.Epoch(0))
	committeeMap := commHandler.buildCommitteeDuties(duties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Same_Slot_SyncCommittee_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	syncDuties.Set(0, []*eth2apiv1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	duties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(nil, duties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Same_Slot(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
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

	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	aDuties, _ := attDuties.Get(0)
	sDuties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Diff_Slot_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for committee duties to be fetched
	currentSlot.Set(phase0.Slot(1))
	ticker.Send(currentSlot.Get())
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for committee duties to be executed
	currentSlot.Set(phase0.Slot(2))
	aDuties, _ := attDuties.Get(0)
	sDuties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Current_Next_Periods(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
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
	attDuties.Set(phase0.Epoch(254), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(256*32 - 49),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	attDuties.Set(phase0.Epoch(255), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(254 * 32),
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})
	syncDuties.Set(0, []*eth2apiv1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	syncDuties.Set(1, []*eth2apiv1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})

	currentSlot.Set(phase0.Slot(256*32 - 49))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for committee duties to be fetched and executed
	aDuties, _ := attDuties.Get(phase0.Epoch(254))
	sDuties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 2, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// STEP 2: wait for committee duty to be executed
	currentSlot.Set(phase0.Slot(256*32 - 48))
	aDuties, _ = attDuties.Get(0)
	sDuties, _ = syncDuties.Get(0)
	committeeMap = commHandler.buildCommitteeDuties(aDuties, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// Validate execution within 1/3 of the slot time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Indices_Changed_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
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

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
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

	// STEP 3: wait for attester duties to be fetched
	currentSlot.Set(phase0.Slot(1))
	ticker.Send(currentSlot.Get())

	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 1
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for committee duties to be executed
	currentSlot.Set(phase0.Slot(2))
	aDuties, _ := attDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties([]*eth2apiv1.AttesterDuty{aDuties[2]}, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Indices_Changed_Attester_Only_2(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
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
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 5},
					},
					ValidatorIndex: 3,
				},
			},
		}
	)

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
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

	// STEP 3: wait for attester duties to be fetched
	currentSlot.Set(phase0.Slot(1))
	ticker.Send(currentSlot.Get())

	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 1
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for committee duties to be executed
	currentSlot.Set(phase0.Slot(2))
	aDuties, _ := attDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties([]*eth2apiv1.AttesterDuty{aDuties[1], aDuties[2]}, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Indices_Changed_Attester_Only_3(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
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
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 5},
					},
					ValidatorIndex: 2,
				},
			},
		}
	)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 0
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger a change in active indices
	scheduler.indicesChg <- struct{}{}
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 5},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})

	// STEP 3: wait for attester duties to be fetched
	currentSlot.Set(phase0.Slot(1))
	ticker.Send(currentSlot.Get())

	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 1
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for committee duties to be executed
	currentSlot.Set(phase0.Slot(2))
	aDuties, _ := attDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Committee_Reorg_Previous_Epoch_Transition_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 1,
				},
			},
		}
	)

	currentSlot.Set(phase0.Slot(63))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(66),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for next epoch attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(64))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg on epoch transition
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(65),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	// wait for attester duties to be fetched again for the current epoch
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 64
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: execute reorged duty
	currentSlot.Set(phase0.Slot(65))
	aDuties, _ := attDuties.Get(phase0.Epoch(2))
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// STEP 6: The first assigned duty should not be executed
	currentSlot.Set(phase0.Slot(66))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed and the indices changed as well
func TestScheduler_Committee_Reorg_Previous_Epoch_Transition_Indices_Changed_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
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

	currentSlot.Set(phase0.Slot(63))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(66),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for next epoch attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(64))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg on epoch transition
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(67),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 64
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	attDuties.Delete(phase0.Epoch(2))

	// STEP 6: wait for attester duties to be fetched again for the current epoch
	currentSlot.Set(phase0.Slot(65))
	ticker.Send(currentSlot.Get())

	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The first assigned duty should not be executed
	currentSlot.Set(phase0.Slot(66))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 8: The reorg assigned duty should not be executed
	currentSlot.Set(phase0.Slot(67))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Committee_Reorg_Previous_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 1,
				},
			},
		}
	)
	attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(35),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched
	currentSlot.Set(phase0.Slot(32))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(33))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(36),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	// wait for attester duties to be fetched again for the current epoch
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: Ticker with no action
	currentSlot.Set(phase0.Slot(34))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: The first assigned duty should not be executed
	currentSlot.Set(phase0.Slot(35))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: execute reorged duty
	currentSlot.Set(phase0.Slot(36))
	aDuties, _ := attDuties.Get(phase0.Epoch(1))
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed and the indices changed the same slot
func TestScheduler_Committee_Reorg_Previous_Indices_Changed_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
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
	attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(35),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched
	currentSlot.Set(phase0.Slot(32))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(33))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(36),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	aDuties, _ := attDuties.Get(phase0.Epoch(1))
	attDuties.Set(phase0.Epoch(1), append(aDuties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(36),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: wait for attester duties to be fetched again for the current epoch
	currentSlot.Set(phase0.Slot(34))
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: The first assigned duty should not be executed
	currentSlot.Set(phase0.Slot(35))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 8: The second and new from indices change assigned duties should be executed
	currentSlot.Set(phase0.Slot(36))
	aDuties, _ = attDuties.Get(phase0.Epoch(1))
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_Committee_Reorg_Current_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
					ValidatorIndex: 1,
				},
			},
		}
	)
	currentSlot.Set(phase0.Slot(48))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(64),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for next epoch attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(49))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(65),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot.Set(phase0.Slot(50))
	ticker.Send(currentSlot.Get())
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: skip to the next epoch
	currentSlot.Set(phase0.Slot(51))
	for slot := currentSlot.Get(); slot < 64; slot++ {
		ticker.Send(slot)
		waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
		currentSlot.Set(slot + 1)
	}

	// STEP 7: The first assigned duty should not be executed
	// slot = 64
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 8: The second assigned duty should be executed
	currentSlot.Set(phase0.Slot(65))
	aDuties, _ := attDuties.Get(phase0.Epoch(2))
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed including indices change in the same slot
func TestScheduler_Committee_Reorg_Current_Indices_Changed_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
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
	currentSlot.Set(phase0.Slot(48))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(48),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for next epoch attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: Ticker with no action
	currentSlot.Set(phase0.Slot(49))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: trigger reorg
	e = &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                     currentSlot.Get(),
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(65),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	scheduler.HandleHeadEvent(logger)(e)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 5: trigger indices change
	scheduler.indicesChg <- struct{}{}
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	aDuties, _ := attDuties.Get(phase0.Epoch(2))
	attDuties.Set(phase0.Epoch(2), append(aDuties, &eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           phase0.Slot(65),
		ValidatorIndex: phase0.ValidatorIndex(2),
	}))
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 6: wait for attester duties to be fetched again for the next epoch due to indices change
	currentSlot.Set(phase0.Slot(50))
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched for current epoch
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for attester duties to be fetched for next epoch
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched for current period
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 7: skip to the next epoch
	currentSlot.Set(phase0.Slot(51))
	for slot := currentSlot.Get(); slot < 64; slot++ {
		ticker.Send(slot)
		waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
		currentSlot.Set(slot + 1)
	}

	// STEP 8: The first assigned duty should not be executed
	// slot = 64
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 9: The second assigned duty should be executed
	currentSlot.Set(phase0.Slot(65))
	aDuties, _ = attDuties.Get(phase0.Epoch(2))
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Early_Block_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	// STEP 1: wait for attester duties to be fetched
	currentSlot.Set(phase0.Slot(0))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.Set(phase0.Slot(1))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration
	startTime := time.Now()
	currentSlot.Set(phase0.Slot(2))
	ticker.Send(currentSlot.Get())

	aDuties, _ := attDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	// STEP 4: trigger head event (block arrival)
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot: currentSlot.Get(),
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)
	require.Less(t, time.Since(startTime), scheduler.network.Beacon.SlotDurationSec()/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Early_Block(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
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
	// STEP 1: wait for attester & sync committee duties to be fetched
	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 2: wait for committee duty to be executed
	aDuties, _ := attDuties.Get(0)
	sDuties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// STEP 3: wait for committee duty to be executed faster than 1/3 of the slot duration
	startTime = time.Now()
	currentSlot.Set(phase0.Slot(2))
	ticker.Send(currentSlot.Get())

	committeeMap = commHandler.buildCommitteeDuties(nil, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	// STEP 4: trigger head event (block arrival)
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot: currentSlot.Get(),
		},
	}
	scheduler.HandleHeadEvent(logger)(e)
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)
	require.Less(t, time.Since(startTime), scheduler.network.Beacon.SlotDurationSec()/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Start_In_The_End_Of_The_Epoch_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	currentSlot.Set(phase0.Slot(31))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(32),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for the next epoch
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for next epoch attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for attester duties to be executed
	currentSlot.Set(phase0.Slot(32))
	aDuties, _ := attDuties.Get(1)
	sDuties, _ := syncDuties.Get(1)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_Fetch_Execute_Next_Epoch_Duty(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(0)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(32),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	currentSlot.Set(phase0.Slot(14))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	// STEP 1: wait for duties to be fetched
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// wait for sync committee duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.Set(phase0.Slot(15))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for duties to be fetched for the next epoch
	currentSlot.Set(phase0.Slot(16))
	ticker.Send(currentSlot.Get())
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: wait for attester duties to be executed
	currentSlot.Set(phase0.Slot(32))
	aDuties, _ := attDuties.Get(1)
	sDuties, _ := syncDuties.Get(1)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_On_Fork_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(2)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)
	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCallAttester, executeDutiesCallAttester := setupDutiesMockAttesterGenesis(scheduler, attDuties)
	fetchDutiesCallSyncCommittee, executeDutiesCallSyncCommittee := setupGenesisDutiesMockSyncCommittee(scheduler, activeShares, syncDuties)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(64),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	aDuties, _ := attDuties.Get(0)
	aExpected := expectedExecutedDutiesAttesterGenesis(attHandler, aDuties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCallAttester, len(aExpected))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	waitForDutiesFetchGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout)
	waitForDutiesFetchGenesis(t, logger, fetchDutiesCallSyncCommittee, executeDutiesCallSyncCommittee, timeout)
	waitForDutiesExecutionGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout, aExpected)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// skip to the next epoch
	currentSlot.Set(phase0.Slot(2))
	for slot := currentSlot.Get(); slot < 48; slot++ {
		ticker.Send(slot)
		waitForNoActionGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout)
		waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
		currentSlot.Set(slot + 1)
	}

	// wait for duties to be fetched for the next fork epoch
	currentSlot.Set(phase0.Slot(48))
	ticker.Send(currentSlot.Get())
	waitForDutiesFetchGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout)

	currentSlot.Set(phase0.Slot(64))
	aDuties, _ = attDuties.Get(2)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 2, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime = time.Now()
	ticker.Send(currentSlot.Get())
	waitForNoActionGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Committee_On_Fork(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(attHandler, syncHandler)
		alanForkEpoch = phase0.Epoch(256)
		currentSlot   = &SafeValue[phase0.Slot]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
				ValidatorIndex: 1,
			},
		}}
	)

	lastPeriodEpoch := phase0.Epoch(256 - 1)
	attDuties.Set(lastPeriodEpoch, []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(lastPeriodEpoch*32 + 1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	attDuties.Set(lastPeriodEpoch+1, []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot((lastPeriodEpoch + 1) * 32),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	syncDuties.Set(1, []*eth2apiv1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	currentSlot.Set(phase0.Slot(lastPeriodEpoch * 32))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot, alanForkEpoch)
	fetchDutiesCallAttester, executeDutiesCallAttester := setupDutiesMockAttesterGenesis(scheduler, attDuties)
	fetchDutiesCallSyncCommittee, executeDutiesCallSyncCommittee := setupGenesisDutiesMockSyncCommittee(scheduler, activeShares, syncDuties)
	fetchDutiesCall, executeDutiesCall := setupDutiesMockCommittee(scheduler, activeShares, attDuties, syncDuties)
	startFn()

	aDuties, _ := attDuties.Get(lastPeriodEpoch)
	aExpected := expectedExecutedDutiesAttesterGenesis(attHandler, aDuties)
	setExecuteGenesisDutyFunc(scheduler, executeDutiesCallAttester, len(aExpected))

	ticker.Send(currentSlot.Get())
	waitForDutiesFetchGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout)
	waitForDutiesFetchGenesis(t, logger, fetchDutiesCallSyncCommittee, executeDutiesCallSyncCommittee, timeout)
	// next period
	waitForDutiesFetchGenesis(t, logger, fetchDutiesCallSyncCommittee, executeDutiesCallSyncCommittee, timeout)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	currentSlot.Set(phase0.Slot(lastPeriodEpoch*32 + 1))
	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout, aExpected)
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	currentSlot.Set(phase0.Slot(lastPeriodEpoch*32 + 2))
	for slot := currentSlot.Get(); slot < 256*32; slot++ {
		ticker.Send(slot)
		if uint64(slot)%32 == scheduler.network.SlotsPerEpoch()/2 {
			waitForDutiesFetchGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout)
		} else {
			waitForNoActionGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout)
			waitForNoActionGenesis(t, logger, fetchDutiesCallSyncCommittee, executeDutiesCallSyncCommittee, timeout)
		}
		waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
		currentSlot.Set(slot + 1)
	}

	currentSlot.Set(phase0.Slot(256 * 32))
	aDuties, _ = attDuties.Get(lastPeriodEpoch + 1)
	sDuties, _ := syncDuties.Get(1)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 256, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)
	waitForNoActionGenesis(t, logger, fetchDutiesCallAttester, executeDutiesCallAttester, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
