package duties

import (
	"bytes"
	"context"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	mocknetwork "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/mocks"
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
			period := s.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
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
							firstEpoch := s.network.Beacon.FirstEpochOfSyncPeriod(period)
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
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
	)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(1),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	duties, _ := attDuties.Get(phase0.Epoch(0))
	committeeMap := commHandler.buildCommitteeDuties(duties, nil, 0, currentSlot.Get())

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

func TestScheduler_Committee_Same_Slot_SyncCommittee_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
	)
	syncDuties.Set(0, []*eth2apiv1.SyncCommitteeDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	duties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(nil, duties, 0, currentSlot.Get())

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

func TestScheduler_Committee_Same_Slot(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
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
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
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

func TestScheduler_Committee_Diff_Slot_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
	)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched using handle initial duties
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 2: wait for no action to be taken
	currentSlot.Set(phase0.Slot(1))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

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

func TestScheduler_Committee_Indices_Changed_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1), activeShare(2), activeShare(3)}
	)

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.Get())
	// no execution should happen in slot 0
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

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
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
	// Wait for the slot ticker to be triggered in the attester, sync committee, and cluster handlers.
	// This ensures that no attester duties are fetched before the cluster ticker is triggered,
	// preventing a scenario where the cluster handler executes duties in the same slot as the attester fetching them.
	time.Sleep(10 * time.Millisecond)

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
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1), activeShare(2), activeShare(3)}
	)

	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.Get())
	// no execution should happen in slot 0
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

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
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
	// Wait for the slot ticker to be triggered in the attester, sync committee, and cluster handlers.
	// This ensures that no attester duties are fetched before the cluster ticker is triggered,
	// preventing a scenario where the cluster handler executes duties in the same slot as the attester fetching them.
	time.Sleep(10 * time.Millisecond)

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
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
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
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.Get())
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
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
	// Wait for the slot ticker to be triggered in the attester, sync committee, and cluster handlers.
	// This ensures that no attester duties are fetched before the cluster ticker is triggered,
	// preventing a scenario where the cluster handler executes duties in the same slot as the attester fetching them.
	time.Sleep(10 * time.Millisecond)

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
func TestScheduler_Committee_Reorg_Previous_Epoch_Transition_Attester_only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
	)

	currentSlot.Set(phase0.Slot(63))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(66),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e.Data.(*eth2apiv1.HeadEvent))
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
	scheduler.HandleHeadEvent(logger)(e.Data.(*eth2apiv1.HeadEvent))
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
func TestScheduler_Committee_Reorg_Previous_Epoch_Transition_Indices_Changed_Attester_only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1), activeShare(2)}
	)

	currentSlot.Set(phase0.Slot(63))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	attDuties.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(66),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched for next epoch
	waitForDuties.Set(true)
	ticker.Send(currentSlot.Get())
	// wait for attester duties to be fetched
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e.Data.(*eth2apiv1.HeadEvent))
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
	scheduler.HandleHeadEvent(logger)(e.Data.(*eth2apiv1.HeadEvent))
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
	// Wait for the slot ticker to be triggered in the attester, sync committee, and cluster handlers.
	// This ensures that no attester duties are fetched before the cluster ticker is triggered,
	// preventing a scenario where the cluster handler executes duties in the same slot as the attester fetching them.
	time.Sleep(10 * time.Millisecond)

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
func TestScheduler_Committee_Reorg_Previous_Attester_only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
	)

	attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(35),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})

	// STEP 1: wait for attester duties to be fetched using handle initial duties
	currentSlot.Set(phase0.Slot(32))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	waitForDuties.Set(true)
	// STEP 2: trigger head event
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot:                      currentSlot.Get(),
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	scheduler.HandleHeadEvent(logger)(e.Data.(*eth2apiv1.HeadEvent))
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
	scheduler.HandleHeadEvent(logger)(e.Data.(*eth2apiv1.HeadEvent))
	// wait for attester duties to be fetched again for the current epoch
	waitForDutiesFetchCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)
	// no execution should happen in slot 33
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

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

func TestScheduler_Committee_Early_Block_Attester_Only(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
	)
	attDuties.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(2),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	})
	// STEP 1: wait for attester duties to be fetched (handle initial duties)
	currentSlot.Set(phase0.Slot(0))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.Set(phase0.Slot(1))
	ticker.Send(currentSlot.Get())
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration when
	// Beacon head event is observed (block arrival)
	currentSlot.Set(phase0.Slot(2))
	aDuties, _ := attDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))
	startTime := time.Now()
	ticker.Send(currentSlot.Get())

	// STEP 4: trigger head event (block arrival)
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot: currentSlot.Get(),
		},
	}
	scheduler.HandleHeadEvent(logger)(e.Data.(*eth2apiv1.HeadEvent))
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
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{activeShare(1)}
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
	currentSlot.Set(phase0.Slot(1))
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 2: wait for committee duty to be executed
	aDuties, _ := attDuties.Get(0)
	sDuties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration when
	// Beacon head event is observed (block arrival)
	currentSlot.Set(phase0.Slot(2))
	committeeMap = commHandler.buildCommitteeDuties(nil, sDuties, 0, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))
	startTime = time.Now()
	ticker.Send(currentSlot.Get())

	// STEP 4: trigger head event (block arrival)
	e := &eth2apiv1.Event{
		Data: &eth2apiv1.HeadEvent{
			Slot: currentSlot.Get(),
		},
	}
	scheduler.HandleHeadEvent(logger)(e.Data.(*eth2apiv1.HeadEvent))
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)
	require.Less(t, time.Since(startTime), scheduler.network.Beacon.SlotDurationSec()/3)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// The purpose of the test is to ensure that the scheduler can handle the case where the indices change
// at the last slot of the epoch, and it does not affect the execution of the duties for the next epoch first slot.
func TestScheduler_Committee_Indices_Changed_At_The_Last_Slot_Of_The_Epoch(t *testing.T) {
	var (
		dutyStore     = dutystore.New()
		attHandler    = NewAttesterHandler(dutyStore.Attester)
		syncHandler   = NewSyncCommitteeHandler(dutyStore.SyncCommittee)
		commHandler   = NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee)
		currentSlot   = &SafeValue[phase0.Slot]{}
		waitForDuties = &SafeValue[bool]{}
		attDuties     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		syncDuties    = hashmap.New[uint64, []*eth2apiv1.SyncCommitteeDuty]()
		activeShares  = []*ssvtypes.SSVShare{
			activeShare(1),
			activeShare(2),
			activeShare(3),
		}
	)
	attDuties.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
		{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(32),
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
		{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(32),
			ValidatorIndex: phase0.ValidatorIndex(2),
		},
	})

	// STEP 1: wait for attester duties to be fetched using handle initial duties
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for no action to be taken
	ticker.Send(currentSlot.Get())
	// no execution should happen in slot 0
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot.Set(phase0.Slot(31))
	ticker.Send(currentSlot.Get())
	// no execution should happen in slot 31
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 3: trigger a change in active indices at the last slot of the epoch
	scheduler.indicesChg <- struct{}{}
	waitForNoActionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout)

	// STEP 4: the first slot of the next epoch duties should be executed as expected
	currentSlot.Set(phase0.Slot(32))

	aDuties, _ := attDuties.Get(1)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, nil, 1, currentSlot.Get())
	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	ticker.Send(currentSlot.Get())
	waitForDutiesExecutionCommittee(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
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
