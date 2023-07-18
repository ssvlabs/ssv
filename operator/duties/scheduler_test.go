package duties

import (
	"context"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/golang/mock/gomock"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"github.com/sourcegraph/conc/pool"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/duties/mocks"
	mockslotticker "github.com/bloxapp/ssv/operator/slot_ticker/mocks"
	mocknetwork "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon/mocks"
)

type mockSlotTicker struct {
	event.Feed
}

func (m *mockSlotTicker) Subscribe(subscriber chan phase0.Slot) event.Subscription {
	return m.Feed.Subscribe(subscriber)
}

func TestScheduler_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	logger := logging.TestLogger(t)

	mockBeaconNode := mocks.NewMockBeaconNode(ctrl)
	mockValidatorController := mocks.NewMockValidatorController(ctrl)
	mockTicker := mockslotticker.NewMockTicker(ctrl)
	// create multiple mock duty handlers
	mockDutyHandler1 := NewMockdutyHandler(ctrl)
	mockDutyHandler2 := NewMockdutyHandler(ctrl)

	opts := &SchedulerOptions{
		Ctx:                 ctx,
		BeaconNode:          mockBeaconNode,
		Network:             networkconfig.TestNetwork,
		ValidatorController: mockValidatorController,
		Ticker:              mockTicker,
		BuilderProposals:    false,
	}

	s := NewScheduler(opts)
	// add multiple mock duty handlers
	s.handlers = []dutyHandler{mockDutyHandler1, mockDutyHandler2}

	mockBeaconNode.EXPECT().Events(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	mockTicker.EXPECT().Subscribe(gomock.Any()).Return(nil).Times(len(s.handlers))

	// setup mock duty handler expectations
	for _, mockDutyHandler := range s.handlers {
		mockDutyHandler.(*MockdutyHandler).EXPECT().Setup(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		mockDutyHandler.(*MockdutyHandler).EXPECT().HandleDuties(gomock.Any(), gomock.Any()).Times(1)
	}

	require.NoError(t, s.Run(ctx, logger, nil))
}

func setupSchedulerAndMocks(t *testing.T, dutiesMap map[phase0.Epoch][]*v1.AttesterDuty, slotToStart phase0.Slot) (
	*Scheduler,
	*mockSlotTicker,
	*zap.Logger,
	chan *spectypes.Duty,
	chan struct{},
	context.CancelFunc,
	*pool.ContextPool,
) {
	ctrl := gomock.NewController(t)

	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.TestLogger(t)

	mockBeaconNode := mocks.NewMockBeaconNode(ctrl)
	mockValidatorController := mocks.NewMockValidatorController(ctrl)
	mockTicker := &mockSlotTicker{}
	mockNetworkConfig := networkconfig.NetworkConfig{
		Beacon: mocknetwork.NewMockNetworkInfo(ctrl),
	}

	executeDutyCalls := make(chan *spectypes.Duty)
	attesterDutiesCalls := make(chan struct{})
	opts := &SchedulerOptions{
		Ctx:                 ctx,
		BeaconNode:          mockBeaconNode,
		Network:             mockNetworkConfig,
		ValidatorController: mockValidatorController,
		ExecuteDuty: func(logger *zap.Logger, duty *spectypes.Duty) {
			logger.Debug("executing duty", zap.Any("duty", duty))
			executeDutyCalls <- duty
		},
		Ticker:           mockTicker,
		BuilderProposals: false,
	}

	s := NewScheduler(opts)
	s.indicesChg = make(chan bool)
	s.handlers = []dutyHandler{NewAttesterHandler()}

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().AttesterDuties(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*v1.AttesterDuty, error) {
			attesterDutiesCalls <- struct{}{}
			return dutiesMap[epoch], nil
		}).AnyTimes()

	s.validatorController.(*mocks.MockValidatorController).EXPECT().ActiveIndices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(logger *zap.Logger, epoch phase0.Epoch) []phase0.ValidatorIndex {
			uniqueIndices := make(map[phase0.ValidatorIndex]bool)

			for _, d := range dutiesMap[epoch] {
				uniqueIndices[d.ValidatorIndex] = true
			}

			indices := make([]phase0.ValidatorIndex, 0, len(uniqueIndices))
			for index := range uniqueIndices {
				indices = append(indices, index)
			}

			return indices
		}).AnyTimes()

	s.beaconNode.(*mocks.MockBeaconNode).EXPECT().SubscribeToCommitteeSubnet(gomock.Any()).Return(nil).AnyTimes()

	mockBeaconNode.EXPECT().Events(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	mockNetworkConfig.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().MinGenesisTime().Return(uint64(0)).AnyTimes()
	mockNetworkConfig.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().SlotDurationSec().Return(12 * time.Second).AnyTimes()
	mockNetworkConfig.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().SlotsPerEpoch().Return(uint64(32)).AnyTimes()
	mockNetworkConfig.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().GetSlotStartTime(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) time.Time {
			return time.Now()
		},
	).AnyTimes()
	mockNetworkConfig.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedEpochAtSlot(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) phase0.Epoch {
			return phase0.Epoch(slot / 32)
		},
	).AnyTimes()

	for range s.handlers {
		mockNetworkConfig.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedCurrentSlot().Return(slotToStart).Times(1)
	}

	schedulerPool := pool.New().WithErrors().WithContext(ctx)
	schedulerReady := make(chan struct{})
	schedulerPool.Go(func(ctx context.Context) error {
		return s.Run(ctx, logger, schedulerReady)
	})
	<-schedulerReady

	return s, mockTicker, logger, executeDutyCalls, attesterDutiesCalls, cancel, schedulerPool
}

// wait for attester duties
func waitForAttesterDuties(t *testing.T, logger *zap.Logger, attesterDutiesCalls chan struct{}, executeDutyCalls chan *spectypes.Duty, timeout time.Duration) {
	select {
	case <-attesterDutiesCalls:
		//logger.Debug("attester duties fetched")
	case <-executeDutyCalls:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for attester duties to be fetched")
	}
}

func waitForNoAction(t *testing.T, logger *zap.Logger, attesterDutiesCalls chan struct{}, executeDutyCalls chan *spectypes.Duty, timeout time.Duration) {
	select {
	case <-attesterDutiesCalls:
		require.FailNow(t, "unexpected attester duties call")
	case <-executeDutyCalls:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		// No action as expected.
	}
}

// wait for duty execution
func waitForDutyExecution(t *testing.T, logger *zap.Logger, attesterDutiesCalls chan struct{}, executeDutyCalls chan *spectypes.Duty, timeout time.Duration) {
	select {
	case <-attesterDutiesCalls:
		require.FailNow(t, "unexpected attester duties call")
	case <-executeDutyCalls:
		//logger.Debug("attester duty executed")
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for attester duty to be executed")
	}
}

func TestScheduler_Attester_Same_Slot(t *testing.T) {
	currentSlot := phase0.Slot(0)
	pubKey := phase0.BLSPubKey{1, 2, 3}
	baseEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[baseEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           0,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	s, mockTicker, logger, executeDutyCalls, attesterDutiesCalls, cancel, schedulerPool := setupSchedulerAndMocks(t, dutiesMap, currentSlot)

	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot
		},
	).AnyTimes()

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched and executed at the same slot
	mockTicker.Send(currentSlot)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)
	waitForDutyExecution(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Diff_Slots(t *testing.T) {
	currentSlot := phase0.Slot(0)
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	s, mockTicker, logger, executeDutyCalls, attesterDutiesCalls, cancel, schedulerPool := setupSchedulerAndMocks(t, dutiesMap, currentSlot)
	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot
		},
	).AnyTimes()

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 2: wait for no action to be taken
	currentSlot = phase0.Slot(1)
	mockTicker.Send(currentSlot)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 3: wait for attester duties to be executed
	currentSlot = phase0.Slot(2)
	mockTicker.Send(currentSlot)
	waitForDutyExecution(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

func TestScheduler_Attester_Indices_Changed(t *testing.T) {
	currentSlot := phase0.Slot(0)
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(0)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	s, mockTicker, logger, executeDutyCalls, attesterDutiesCalls, cancel, schedulerPool := setupSchedulerAndMocks(t, dutiesMap, currentSlot)
	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot
		},
	).AnyTimes()

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 2: trigger a change in active indices
	s.indicesChg <- true
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 3: wait for attester duties to be fetched again
	currentSlot = phase0.Slot(1)
	dutiesMap[currentEpoch] = append(dutiesMap[currentEpoch], &v1.AttesterDuty{
		PubKey:         phase0.BLSPubKey{1, 2, 4},
		Slot:           2,
		ValidatorIndex: phase0.ValidatorIndex(2),
	})
	mockTicker.Send(currentSlot)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 4: wait for attester duties to be executed
	currentSlot = phase0.Slot(2)
	mockTicker.Send(currentSlot)
	waitForDutyExecution(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)
	waitForDutyExecution(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Attester_Reorg_Previous_Epoch_Transition(t *testing.T) {
	currentSlot := phase0.Slot(63)
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(1)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch+1] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           64 + 2,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	s, mockTicker, logger, executeDutyCalls, attesterDutiesCalls, cancel, schedulerPool := setupSchedulerAndMocks(t, dutiesMap, currentSlot)
	handleEventFunc := s.HandleHeadEvent(logger)
	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot
		},
	).AnyTimes()

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched for current and next epoch
	mockTicker.Send(currentSlot) // slot = 63
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                      currentSlot,
			CurrentDutyDependentRoot:  phase0.Root{0x01},
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	handleEventFunc(e)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 3: Ticker with no action
	currentSlot++ // slot = 64
	mockTicker.Send(currentSlot)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 4: trigger reorg on epoch transition
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                      currentSlot,
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	handleEventFunc(e)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot++ // slot = 65
	dutiesMap[currentEpoch+1] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           64 + 3,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	mockTicker.Send(currentSlot)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 6: The first assigned duty should not be executed
	currentSlot++ // slot = 66
	mockTicker.Send(currentSlot)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 7: The second assigned duty should be executed
	currentSlot++ // slot = 36
	mockTicker.Send(currentSlot)
	waitForDutyExecution(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg previous dependent root changed
func TestScheduler_Attester_Reorg_Previous(t *testing.T) {
	currentSlot := phase0.Slot(32)
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(1)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           32 + 3,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	s, mockTicker, logger, executeDutyCalls, attesterDutiesCalls, cancel, schedulerPool := setupSchedulerAndMocks(t, dutiesMap, currentSlot)
	handleEventFunc := s.HandleHeadEvent(logger)
	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot
		},
	).AnyTimes()

	timeout := 100 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot) // slot = 32
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                      currentSlot,
			PreviousDutyDependentRoot: phase0.Root{0x01},
		},
	}
	handleEventFunc(e)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 3: Ticker with no action
	currentSlot++ // slot = 33
	mockTicker.Send(currentSlot)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                      currentSlot,
			PreviousDutyDependentRoot: phase0.Root{0x02},
		},
	}
	handleEventFunc(e)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot++ // slot = 34
	dutiesMap[currentEpoch] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           32 + 4,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	mockTicker.Send(currentSlot)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 6: The first assigned duty should not be executed
	currentSlot++ // slot = 35
	mockTicker.Send(currentSlot)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 7: The second assigned duty should be executed
	currentSlot++ // slot = 36
	mockTicker.Send(currentSlot)
	waitForDutyExecution(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}

// reorg current dependent root changed
func TestScheduler_Attester_Reorg_Current(t *testing.T) {
	currentSlot := phase0.Slot(47)
	pubKey := phase0.BLSPubKey{1, 2, 3}
	currentEpoch := phase0.Epoch(1)

	dutiesMap := make(map[phase0.Epoch][]*v1.AttesterDuty)
	dutiesMap[currentEpoch+1] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           64,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}

	s, mockTicker, logger, executeDutyCalls, attesterDutiesCalls, cancel, schedulerPool := setupSchedulerAndMocks(t, dutiesMap, currentSlot)
	handleEventFunc := s.HandleHeadEvent(logger)
	s.network.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot
		},
	).AnyTimes()

	timeout := 50 * time.Millisecond

	// STEP 1: wait for attester duties to be fetched
	mockTicker.Send(currentSlot) // slot = 47
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 2: trigger head event
	e := &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot,
			CurrentDutyDependentRoot: phase0.Root{0x01},
		},
	}
	handleEventFunc(e)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 3: Ticker with no action
	currentSlot++ // slot = 48
	mockTicker.Send(currentSlot)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 4: trigger reorg
	e = &v1.Event{
		Data: &v1.HeadEvent{
			Slot:                     currentSlot,
			CurrentDutyDependentRoot: phase0.Root{0x02},
		},
	}
	handleEventFunc(e)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 5: wait for attester duties to be fetched again for the current epoch
	currentSlot++ // slot = 49
	dutiesMap[currentEpoch+1] = []*v1.AttesterDuty{
		{
			PubKey:         pubKey,
			Slot:           65,
			ValidatorIndex: phase0.ValidatorIndex(1),
		},
	}
	mockTicker.Send(currentSlot)
	waitForAttesterDuties(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 6: skip to the next epoch
	for currentSlot = 50; currentSlot < 64; currentSlot++ {
		mockTicker.Send(currentSlot)
		waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)
	}

	// STEP 7: The first assigned duty should not be executed
	// slot = 64
	mockTicker.Send(currentSlot)
	waitForNoAction(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// STEP 8: The second assigned duty should be executed
	currentSlot++ // slot = 65
	mockTicker.Send(currentSlot)
	waitForDutyExecution(t, logger, attesterDutiesCalls, executeDutyCalls, timeout)

	// Stop scheduler & wait for graceful exit.
	cancel()
	require.NoError(t, schedulerPool.Wait())
}
