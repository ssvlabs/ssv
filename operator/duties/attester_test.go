package duties

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
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
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func TestAttesterHandlerShouldExecuteRespectsMinParticipationEpoch(t *testing.T) {
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

	handler := NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
	handler.logger = zap.NewNop()
	handler.netCfg = &netCfg
	handler.validatorProvider = validatorProvider

	shouldExecute := handler.shouldExecute(&eth2apiv1.AttesterDuty{
		PubKey:         phase0.BLSPubKey(share.ValidatorPubKey),
		Slot:           0,
		ValidatorIndex: share.ValidatorIndex,
	})

	require.False(t, shouldExecute)
}

func setupAttesterDutiesMock(
	s *Scheduler,
	dutiesMap *hashmap.Map[phase0.Epoch, []*eth2apiv1.AttesterDuty],
	waitForDuties *SafeValue[bool],
) (chan struct{}, chan []*spectypes.ValidatorDuty) {
	return setupAttesterDutiesMockWithFetcher(s, dutiesMap, waitForDuties, nil)
}

func setupAttesterDutiesMockWithFetcher(
	s *Scheduler,
	dutiesMap *hashmap.Map[phase0.Epoch, []*eth2apiv1.AttesterDuty],
	waitForDuties *SafeValue[bool],
	fetcher func(context.Context, phase0.Epoch, []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error),
) (chan struct{}, chan []*spectypes.ValidatorDuty) {
	// fetchDutiesCall relays/signals duty-fetch calls, it is buffered so that our test code can run in a single
	// go-routine (so that we don't need to worry about draining this channel to let the execution proceed). The
	// buffer size should be large enough for the test to not block.
	fetchDutiesCall := make(chan struct{}, 100)
	// executeDutiesCall is similar to fetchDutiesCall but signals the duty-executions.
	executeDutiesCall := make(chan []*spectypes.ValidatorDuty, 100)

	if fetcher == nil {
		fetcher = func(_ context.Context, epoch phase0.Epoch, _ []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
			duties, _ := dutiesMap.Get(epoch)
			return duties, nil
		}
	}

	s.beaconNode.(*MockBeaconNode).EXPECT().AttesterDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
			if waitForDuties.Get() {
				fetchDutiesCall <- struct{}{}
			}
			return fetcher(ctx, epoch, indices)
		}).AnyTimes()

	getShares := func() []*types.SSVShare {
		var attestingShares []*types.SSVShare
		dutiesMap.Range(func(epoch phase0.Epoch, duties []*eth2apiv1.AttesterDuty) bool {
			uniqueIndices := make(map[phase0.ValidatorIndex]bool)

			for _, d := range duties {
				uniqueIndices[d.ValidatorIndex] = true
			}

			for index := range uniqueIndices {
				attestingShare := &types.SSVShare{
					Share: spectypes.Share{
						ValidatorIndex: index,
					},
					ActivationEpoch: epoch,
					Liquidated:      false,
					// this particular status is needed so that ActivationEpoch can be taken into consideration when checking the IsAttesting() condition.
					Status: eth2apiv1.ValidatorStatePendingQueued,
				}
				attestingShares = append(attestingShares, attestingShare)
			}
			return true
		})

		return attestingShares
	}

	s.validatorProvider.(*MockValidatorProvider).EXPECT().SelfValidators().DoAndReturn(getShares).AnyTimes()
	s.validatorProvider.(*MockValidatorProvider).EXPECT().Validator(gomock.Any()).DoAndReturn(
		func(pubKey []byte) (*types.SSVShare, bool) {
			var ssvShare *types.SSVShare
			var minEpoch phase0.Epoch
			dutiesMap.Range(func(epoch phase0.Epoch, duties []*eth2apiv1.AttesterDuty) bool {
				for _, duty := range duties {
					if bytes.Equal(duty.PubKey[:], pubKey) {
						ssvShare = &types.SSVShare{
							Share: spectypes.Share{
								ValidatorIndex: duty.ValidatorIndex,
							},
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

			if ssvShare != nil {
				return ssvShare, true
			}

			return nil, false
		},
	).AnyTimes()

	s.beaconNode.(*MockBeaconNode).EXPECT().SubmitBeaconCommitteeSubscriptions(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return fetchDutiesCall, executeDutiesCall
}

func expectedExecutedAttesterDuties(handler *AttesterHandler, duties []*eth2apiv1.AttesterDuty) []*spectypes.ValidatorDuty {
	expectedDuties := make([]*spectypes.ValidatorDuty, 0, len(duties))
	for _, d := range duties {
		expectedDuties = append(expectedDuties, handler.toSpecDuty(d, spectypes.BNRoleAggregator))
	}
	return expectedDuties
}

func waitForFetchedAttesterEpoch(
	t *testing.T,
	fetchDutiesCall chan struct{},
	fetchedEpochs chan phase0.Epoch,
	timeout time.Duration,
	expectedEpoch phase0.Epoch,
) {
	waitForDutiesFetch(t, fetchDutiesCall, timeout)

	select {
	case epoch := <-fetchedEpochs:
		require.Equal(t, expectedEpoch, epoch)
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for fetched attester epoch")
	}
}

func TestScheduler_Attester_Same_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler   = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
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
		waitForSlotN(scheduler.netCfg.Beacon, 1)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Diff_Slots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler   = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
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
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(2))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken on slot 0
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: trigger a change in active indices & wait for attester duties to be re-fetched on slot 0
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
		scheduler.indicesChgCh <- struct{}{}
		waitForDuties.Set(true)
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		// no other fetching or execution should happen on slot 0
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for attester duties to be executed on slot 1
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[1]})
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

func TestScheduler_Attester_Multiple_Indices_Changed_Same_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: wait for no action to be taken
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for no action to be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: trigger a change in active indices
		scheduler.indicesChgCh <- struct{}{}
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.AttesterDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 3},
			Slot:           phase0.Slot(3),
			ValidatorIndex: phase0.ValidatorIndex(1),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger a change in active indices in the same slot
		scheduler.indicesChgCh <- struct{}{}
		duties, _ = dutiesMap.Get(phase0.Epoch(0))
		dutiesMap.Set(phase0.Epoch(0), append(duties, &eth2apiv1.AttesterDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(4),
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: wait for attester duties to be fetched
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		waitForDuties.Set(true)
		ticker.Send(phase0.Slot(2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 6: wait for attester duties to be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(3))
		duties, _ = dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[0]})
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(3))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// STEP 7: wait for attester duties to be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(4))
		duties, _ = dutiesMap.Get(phase0.Epoch(0))
		expected = expectedExecutedAttesterDuties(handler, []*eth2apiv1.AttesterDuty{duties[1]})
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
func TestScheduler_Attester_Reorg_Previous_Epoch_Transition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)

		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch*2-1)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch*2-1)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for attester duties to be fetched for the current epoch
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

		// STEP 3: Ticker with no action
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on epoch transition
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch * 2,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: wait for attester duties to be re-fetched for the current epoch
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: The first assigned duty should not be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 7: The second assigned duty should be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+3))
		duties, _ := dutiesMap.Get(phase0.Epoch(2))
		expected := expectedExecutedAttesterDuties(handler, duties)
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
func TestScheduler_Attester_Reorg_Previous_Epoch_Transition_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch*2-1)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch*2-1)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for attester duties to be fetched for the current epoch
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

		// STEP 3: Ticker with no action
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg on epoch transition
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch * 2,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: trigger indices change
		scheduler.indicesChgCh <- struct{}{}
		duties, _ := dutiesMap.Get(phase0.Epoch(2))
		dutiesMap.Set(phase0.Epoch(2), append(duties, &eth2apiv1.AttesterDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 3),
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: wait for attester duties to be re-fetched for the current epoch
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 7: The first assigned duty should not be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The second assigned duty should be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+3))
		duties, _ = dutiesMap.Get(phase0.Epoch(2))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 3))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed
func TestScheduler_Attester_Reorg_Previous(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for attester duties to be fetched (handle initial duties)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
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
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+1))
		waitForDuties.Set(true)
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch + 1,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 4),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: wait for no action to be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: The first assigned duty should not be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+3))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 3))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 7: The second assigned duty should be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+4))
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch + 4))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg previous dependent root changed and the indices changed the same slot
func TestScheduler_Attester_Reorg_Previous_Indices_Change_Same_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 3),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: wait for attester duties to be fetched (handle initial duties)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
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
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+1))
		waitForDuties.Set(true)
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch + 1,
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 4),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: trigger indices change
		scheduler.indicesChgCh <- struct{}{}
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		dutiesMap.Set(phase0.Epoch(1), append(duties, &eth2apiv1.AttesterDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(testSlotsPerEpoch + 4),
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 6: wait for attester duties to be re-fetched for the current epoch
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 7: The first assigned duty should not be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+3))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 3))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The second and new from indices change assigned duties should be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+4))
		duties, _ = dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch + 4))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

// reorg current dependent root changed
func TestScheduler_Attester_Reorg_Current(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch+testSlotsPerEpoch/2)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch+testSlotsPerEpoch/2)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(0 + testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for attester duties to be fetched for the current epoch
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
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2 + 1,
				CurrentDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: wait for attester duties to be re-fetched for the current epoch
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 6: skip to the next epoch
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+3))
		for slot := phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 3); slot < testSlotsPerEpoch*2; slot++ {
			ticker.Send(slot)
			waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
			waitForSlotN(scheduler.netCfg.Beacon, slot+1)
		}

		// STEP 7: The first assigned duty should not be executed
		// slot = testSlotsPerEpoch*2
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The second assigned duty should be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+1))
		duties, _ := dutiesMap.Get(phase0.Epoch(2))
		expected := expectedExecutedAttesterDuties(handler, duties)
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
func TestScheduler_Attester_Reorg_Current_Indices_Changed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch+testSlotsPerEpoch/2)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch+testSlotsPerEpoch/2)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(0 + testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for attester duties to be fetched for the current epoch
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
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: trigger reorg & check no action is taken in the current slot since it only affects the next epoch
		// (we'll fetch the duties for the next epoch on the next slot-tick).
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                     testSlotsPerEpoch + testSlotsPerEpoch/2 + 1,
				CurrentDutyDependentRoot: phase0.Root{0x02},
			},
		}
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 2))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 5: trigger indices change & wait for:
		// - on the current slot, attester duties to be re-fetched for the current epoch (due to indices change)
		// - on the next slot, attester duties to be re-fetched for the next epoch (due to indices change)
		duties, _ := dutiesMap.Get(phase0.Epoch(2))
		dutiesMap.Set(phase0.Epoch(2), append(duties, &eth2apiv1.AttesterDuty{
			PubKey:         phase0.BLSPubKey{1, 2, 4},
			Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
			ValidatorIndex: phase0.ValidatorIndex(2),
		}))
		scheduler.indicesChgCh <- struct{}{}
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+3))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 3))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+testSlotsPerEpoch/2+4))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 4))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 6: skip to the next epoch
		for slot := phase0.Slot(testSlotsPerEpoch + testSlotsPerEpoch/2 + 5); slot < testSlotsPerEpoch*2; slot++ {
			waitForSlotN(scheduler.netCfg.Beacon, slot)
			ticker.Send(slot)
			waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		}

		// STEP 7: The first assigned duty should not be executed
		// slot = testSlotsPerEpoch*2
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 8: The second assigned duty should be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+1))
		duties, _ = dutiesMap.Get(phase0.Epoch(2))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Early_Block(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler   = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
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
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, &SafeValue[bool]{})
		require.NoError(t, scheduler.Start(ctx))

		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for no action to be taken
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: wait for attester duties to be executed faster than 1/3 of the slot duration when
		// Beacon head event is observed (block arrival)
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedAttesterDuties(handler, duties)
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
		require.Less(t, time.Since(slotStartTime), scheduler.netCfg.SlotDuration/3)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Start_At_The_Last_Slot_Of_The_Epoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch-1)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch-1)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)

		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// STEP 1: (on startup) wait for attester duties to be fetched for the next epoch
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForDutiesFetch(t, fetchDutiesCall, timeout) // next epoch fetch-call

		// STEP 2: wait for attester duties to be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch))
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Indices_Changed_Too_Late_In_Slot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// A duty exists from the start, so there's an eligible validator (attester shares are derived from the
		// duties map) and the silent startup fetch fulfils the current-epoch intent. That settles the epoch
		// before the indices change, isolating what we test: a late indices change is the only slot-1 re-fetch.
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
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
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		// STEP 1: the startup fetch already fulfilled the current-epoch intent, so slot 0 has no action.
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: arrange for indices change to arrive too late for slot 0 processing.
		waitForDuties.Set(true)
		go func() {
			time.Sleep(scheduler.netCfg.IntervalDuration(0) + 1*time.Millisecond)
			scheduler.indicesChgCh <- struct{}{}
		}()

		// No fetching should happen on slot 0 because the indices change arrived too late in the slot.
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: on slot 1 the deferred indices change is processed and duties are re-fetched.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(1))
		ticker.Send(phase0.Slot(1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: on slot 2 the fetched duty executes.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(2))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Reorg_Current_Epoch_Transition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)

		initialEpoch2Duties := []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 1),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		}
		dutiesMap.Set(phase0.Epoch(2), initialEpoch2Duties)
		dutiesMap.Set(phase0.Epoch(3), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{4, 5, 6},
				Slot:           phase0.Slot(testSlotsPerEpoch * 3),
				ValidatorIndex: phase0.ValidatorIndex(2),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch*2-1)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch*2-1)
		fetchedEpochs := make(chan phase0.Epoch, 100)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMockWithFetcher(
			scheduler,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
				fetchedEpochs <- epoch
				duties, _ := dutiesMap.Get(epoch)
				return duties, nil
			},
		)

		// STEP 1: startup at the last slot fetches duties for epoch 2.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(2))

		// STEP 2: establish the baseline head state for the previous epoch.
		e := &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch*2 - 1,
				Block:                     phase0.Root{0x03},
				CurrentDutyDependentRoot:  phase0.Root{0x02},
				PreviousDutyDependentRoot: phase0.Root{0x01},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: enter the new epoch with no extra action.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch * 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: current-duty-dependent-root changes during epoch transition.
		// For attester this should not refetch the current epoch, only schedule the next epoch for refetch.
		dutiesMap.Set(phase0.Epoch(2), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch*2 + 2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})
		dutiesMap.Set(phase0.Epoch(3), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{4, 5, 6},
				Slot:           phase0.Slot(testSlotsPerEpoch*3 + 1),
				ValidatorIndex: phase0.ValidatorIndex(2),
			},
		})
		e = &eth2apiv1.Event{
			Data: &eth2apiv1.HeadEvent{
				Slot:                      testSlotsPerEpoch * 2,
				Block:                     phase0.Root{0x05},
				CurrentDutyDependentRoot:  phase0.Root{0x04},
				PreviousDutyDependentRoot: phase0.Root{0x02},
			},
		}
		scheduler.HandleHeadEvent()(t.Context(), e.Data.(*eth2apiv1.HeadEvent))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: the already-fetched current-epoch duty still executes on slot 25, proving no current-epoch refetch.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+1))
		expectedCurrent := expectedExecutedAttesterDuties(handler, initialEpoch2Duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expectedCurrent))

		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expectedCurrent)

		// STEP 6: next-epoch refetch still happens later at the normal prefetch point.
		for slot := phase0.Slot(testSlotsPerEpoch*2 + 2); slot < phase0.Slot(testSlotsPerEpoch*2+5); slot++ {
			waitForSlotN(scheduler.netCfg.Beacon, slot)
			ticker.Send(slot)
			waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		}
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch*2+5))
		ticker.Send(phase0.Slot(testSlotsPerEpoch*2 + 5))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(3))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Retry_Current_Epoch_Fetch_On_Next_Tick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler             = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap           = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties       = &SafeValue[bool]{}
			currentEpochFetches atomic.Int32
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
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
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMockWithFetcher(
			scheduler,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
				duties, _ := dutiesMap.Get(epoch)
				if epoch == phase0.Epoch(0) && currentEpochFetches.Add(1) <= 3 {
					return nil, errors.New("failed to fetch attester duties")
				}
				return duties, nil
			},
		)

		// STEP 1: fail the initial current epoch fetch.
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

		// STEP 4: retry again on slot 2, succeed, and execute the duty in the same slot.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(2))
		duties, _ := dutiesMap.Get(phase0.Epoch(0))
		expected := expectedExecutedAttesterDuties(handler, duties)
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

func TestScheduler_Attester_No_Eligible_Validators_Leaves_Current_Epoch_Fetch_Pending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocks(ctx, t, []dutyHandler{handler})
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)

		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))

		// With no eligible validators, the startup fetch is a no-op that must NOT mark the intent fulfilled —
		// otherwise the duty would never be fetched once a validator becomes eligible (e.g. after a metadata
		// sync that lands without an accompanying indices-change event). The intent stays pending.
		require.False(t, handler.dutyFetchIntents[phase0.Epoch(0)])
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// The next tick re-evaluates the pending intent. There are still no eligible validators, so it
		// short-circuits before any fetch and the intent remains pending (ready to be retried later).
		ticker.Send(phase0.Slot(0))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)
		require.False(t, handler.dutyFetchIntents[phase0.Epoch(0)])

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Retry_Next_Epoch_Fetch_On_Next_Tick_Mid_Epoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler          = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap        = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties    = &SafeValue[bool]{}
			nextEpochFetches atomic.Int32
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{9, 9, 9},
				Slot:           phase0.Slot(0),
				ValidatorIndex: phase0.ValidatorIndex(9),
			},
		})
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch/2)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch/2)
		fetchedEpochs := make(chan phase0.Epoch, 100)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMockWithFetcher(
			scheduler,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
				fetchedEpochs <- epoch
				duties, _ := dutiesMap.Get(epoch)
				if epoch == phase0.Epoch(1) && nextEpochFetches.Add(1) <= 3 {
					return nil, errors.New("failed to fetch attester duties")
				}
				return duties, nil
			},
		)

		// STEP 1: on startup, fetch duties for the current epoch successfully.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(0))

		// STEP 2: on the first tick, fail to fetch duties for the next epoch.
		ticker.Send(phase0.Slot(testSlotsPerEpoch / 2))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch/2+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch/2 + 1))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch/2+2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch/2 + 2))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: retry on the next tick and succeed.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch/2+3))
		ticker.Send(phase0.Slot(testSlotsPerEpoch/2 + 3))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Retry_Next_Epoch_Fetch_On_Next_Tick_Epoch_Transition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler          = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap        = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties    = &SafeValue[bool]{}
			nextEpochFetches atomic.Int32
		)
		dutiesMap.Set(phase0.Epoch(0), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{9, 9, 9},
				Slot:           phase0.Slot(0),
				ValidatorIndex: phase0.ValidatorIndex(9),
			},
		})
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch-3)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch-3)
		fetchedEpochs := make(chan phase0.Epoch, 100)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMockWithFetcher(
			scheduler,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
				fetchedEpochs <- epoch
				duties, _ := dutiesMap.Get(epoch)
				if epoch == phase0.Epoch(1) && nextEpochFetches.Add(1) <= 3 {
					return nil, errors.New("failed to fetch attester duties")
				}
				return duties, nil
			},
		)

		// STEP 1: on startup, fetch duties for the current epoch successfully.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(0))

		// STEP 2: on the first tick, fail to fetch duties for the next epoch.
		ticker.Send(phase0.Slot(testSlotsPerEpoch - 3))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch-2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch - 2))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch-1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch - 1))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 5: on the next tick, the previously next epoch becomes current, fetch succeeds, and the duty executes.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch))
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Retry_Next_Epoch_Fetch_On_Next_Tick_Epoch_Transition_Start_At_Last_Slot_Of_Current_Epoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler          = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap        = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties    = &SafeValue[bool]{}
			nextEpochFetches atomic.Int32
		)
		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
			{
				PubKey:         phase0.BLSPubKey{1, 2, 3},
				Slot:           phase0.Slot(testSlotsPerEpoch + 2),
				ValidatorIndex: phase0.ValidatorIndex(1),
			},
		})

		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch-1)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch-1)
		fetchedEpochs := make(chan phase0.Epoch, 100)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMockWithFetcher(
			scheduler,
			dutiesMap,
			waitForDuties,
			func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
				fetchedEpochs <- epoch
				duties, _ := dutiesMap.Get(epoch)
				if epoch == phase0.Epoch(1) && nextEpochFetches.Add(1) <= 3 {
					return nil, errors.New("failed to fetch attester duties")
				}
				return duties, nil
			},
		)

		// STEP 1: starting at the last slot of the current epoch fetches the next epoch immediately, and it fails.
		waitForDuties.Set(true)
		require.NoError(t, scheduler.Start(ctx))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))

		// STEP 2: on the epoch transition tick, retry the previously next epoch as the now current epoch and fail.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch))
		ticker.Send(phase0.Slot(testSlotsPerEpoch))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 3: retry on the next tick and fail again.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+1))
		ticker.Send(phase0.Slot(testSlotsPerEpoch + 1))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 4: retry on the next tick, succeed, and execute the duty in the same slot.
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch+2))
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch + 2))
		waitForFetchedAttesterEpoch(t, fetchDutiesCall, fetchedEpochs, timeout, phase0.Epoch(1))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}

func TestScheduler_Attester_Fetch_Execute_Next_Epoch_Duty(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			handler       = NewAttesterHandler(dutystore.NewDuties[eth2apiv1.AttesterDuty](), false)
			dutiesMap     = hashmap.New[phase0.Epoch, []*eth2apiv1.AttesterDuty]()
			waitForDuties = &SafeValue[bool]{}
		)
		// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
		// This deadline needs to be large enough to not prevent tests from executing their intended flow.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		scheduler, ticker := setupSchedulerAndMocksWithStartSlot(ctx, t, []dutyHandler{handler}, testSlotsPerEpoch/2-3)
		waitForSlotN(scheduler.netCfg.Beacon, testSlotsPerEpoch/2-3)
		fetchDutiesCall, executeDutiesCall := setupAttesterDutiesMock(scheduler, dutiesMap, waitForDuties)
		require.NoError(t, scheduler.Start(ctx))

		dutiesMap.Set(phase0.Epoch(1), []*eth2apiv1.AttesterDuty{
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
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch/2-2))
		ticker.Send(phase0.Slot(testSlotsPerEpoch/2 - 2))
		waitForNoAction(t, fetchDutiesCall, executeDutiesCall, noActionTimeout)

		// STEP 2: wait for duties to be fetched for the next epoch
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch/2-1))
		waitForDuties.Set(true)
		ticker.Send(phase0.Slot(testSlotsPerEpoch/2 - 1))
		waitForDutiesFetch(t, fetchDutiesCall, timeout)

		// STEP 3: wait for attester duties to be executed
		waitForSlotN(scheduler.netCfg.Beacon, phase0.Slot(testSlotsPerEpoch))
		duties, _ := dutiesMap.Get(phase0.Epoch(1))
		expected := expectedExecutedAttesterDuties(handler, duties)
		setExecuteDutyFunc(scheduler, executeDutiesCall, len(expected))

		ticker.Send(phase0.Slot(testSlotsPerEpoch))
		waitForDutiesExecution(t, fetchDutiesCall, executeDutiesCall, timeout, expected)

		// Stop scheduler & wait for graceful exit.
		cancel()
		require.NoError(t, scheduler.Wait())
		ticker.WaitShutdown()
	})
}
