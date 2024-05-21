package duties

import (
	"context"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cornelk/hashmap"
	"github.com/golang/mock/gomock"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/duties/mocks"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	mocknetwork "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/mocks"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
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

	s.validatorProvider.(*mocks.MockValidatorProvider).EXPECT().Validator(gomock.Any()).DoAndReturn(
		func(pubKey []byte) *ssvtypes.SSVShare {
			return &ssvtypes.SSVShare{
				Share: spectypes.Share{
					Committee: []*spectypes.ShareMember{
						{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
					},
				},
			}
		},
	).AnyTimes()

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

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().AttesterDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			duties, _ := attDuties.Get(epoch)
			return duties, nil
		}).AnyTimes()

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SyncCommitteeDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			period := s.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			duties, _ := syncDuties.Get(period)
			return duties, nil
		}).AnyTimes()

	s.validatorProvider.(*mocks.MockValidatorProvider).EXPECT().SelfParticipatingValidators(gomock.Any()).Return(activeShares).AnyTimes()
	s.validatorProvider.(*mocks.MockValidatorProvider).EXPECT().ParticipatingValidators(gomock.Any()).Return(activeShares).AnyTimes()

	s.validatorController.(*mocks.MockValidatorController).EXPECT().AllActiveIndices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(epoch phase0.Epoch, afterInit bool) []phase0.ValidatorIndex {
			return indicesFromShares(activeShares)
		}).AnyTimes()

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SubmitBeaconCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SubmitSyncCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

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
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
			},
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Index: 1,
				},
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
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocksCommittee(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	duties, _ := attDuties.Get(phase0.Epoch(0))
	committeeMap := commHandler.buildCommitteeDuties(duties, nil, 0, currentSlot.Get())

	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())

	waitForCommitteeDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

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
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
			},
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Index: 1,
				},
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
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocksCommittee(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	duties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(nil, duties, 0, currentSlot.Get())

	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())

	waitForCommitteeDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

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
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
			},
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Index: 1,
				},
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
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocksCommittee(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
	fetchDutiesCall, executeDutiesCall := setupCommitteeDutiesMock(scheduler, activeShares, attDuties, syncDuties, waitForDuties)
	startFn()

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	aDuties, _ := attDuties.Get(0)
	sDuties, _ := syncDuties.Get(0)
	committeeMap := commHandler.buildCommitteeDuties(aDuties, sDuties, 0, currentSlot.Get())

	setExecuteDutyFuncs(scheduler, executeDutiesCall, len(committeeMap))

	startTime := time.Now()
	ticker.Send(currentSlot.Get())

	waitForCommitteeDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

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
		activeShares  = []*ssvtypes.SSVShare{{
			Share: spectypes.Share{
				Committee: []*spectypes.ShareMember{
					{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4},
				},
			},
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Index: 1,
				},
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

	// STEP 1: wait for attester duties to be fetched using handle initial duties
	scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocksCommittee(t, []dutyHandler{attHandler, syncHandler, commHandler}, currentSlot)
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
	waitForCommitteeDutiesExecution(t, logger, fetchDutiesCall, executeDutiesCall, timeout, committeeMap)

	// validate the 1/3 of the slot waiting time
	require.Less(t, scheduler.network.Beacon.SlotDurationSec()/3, time.Since(startTime))

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
