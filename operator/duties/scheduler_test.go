package duties

import (
	"context"
	"sync"
	"testing"
	"time"

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

func setupSchedulerAndMocks(t *testing.T, handler dutyHandler, currentSlot *SlotValue) (
	*Scheduler,
	*zap.Logger,
	*mockSlotTicker,
	time.Duration,
	context.CancelFunc,
	*pool.ContextPool,
) {
	ctrl := gomock.NewController(t)
	timeout := 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.TestLogger(t)

	mockBeaconNode := mocks.NewMockBeaconNode(ctrl)
	mockValidatorController := mocks.NewMockValidatorController(ctrl)
	mockTicker := &mockSlotTicker{}
	mockNetworkConfig := networkconfig.NetworkConfig{
		Beacon: mocknetwork.NewMockBeaconNetwork(ctrl),
	}

	opts := &SchedulerOptions{
		Ctx:                 ctx,
		BeaconNode:          mockBeaconNode,
		Network:             mockNetworkConfig,
		ValidatorController: mockValidatorController,
		Ticker:              mockTicker,
		BuilderProposals:    false,
	}

	s := NewScheduler(opts)
	s.blockPropagateDelay = 1 * time.Millisecond
	s.indicesChg = make(chan struct{})
	s.handlers = []dutyHandler{handler}

	mockBeaconNode.EXPECT().Events(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	mockNetworkConfig.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().MinGenesisTime().Return(uint64(0)).AnyTimes()
	mockNetworkConfig.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().SlotDurationSec().Return(150 * time.Millisecond).AnyTimes()
	mockNetworkConfig.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().SlotsPerEpoch().Return(uint64(32)).AnyTimes()
	mockNetworkConfig.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().GetSlotStartTime(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) time.Time {
			return time.Now()
		},
	).AnyTimes()
	mockNetworkConfig.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().EstimatedEpochAtSlot(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) phase0.Epoch {
			return phase0.Epoch(uint64(slot) / s.network.SlotsPerEpoch())
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().EstimatedCurrentSlot().DoAndReturn(
		func() phase0.Slot {
			return currentSlot.GetSlot()
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().EstimatedCurrentEpoch().DoAndReturn(
		func() phase0.Epoch {
			return phase0.Epoch(uint64(currentSlot.GetSlot()) / s.network.SlotsPerEpoch())
		},
	).AnyTimes()

	s.network.Beacon.(*mocknetwork.MockBeaconNetwork).EXPECT().EpochsPerSyncCommitteePeriod().Return(uint64(256)).AnyTimes()

	err := s.Start(ctx, logger)
	require.NoError(t, err)

	// Create a pool to wait for the scheduler to finish.
	schedulerPool := pool.New().WithErrors().WithContext(ctx)
	schedulerPool.Go(func(ctx context.Context) error {
		return s.Wait()
	})

	return s, logger, mockTicker, timeout, cancel, schedulerPool
}

func setExecuteDutyFunc(s *Scheduler, executeDutiesCall chan []*spectypes.Duty, executeDutiesCallSize int) {
	executeDutiesBuffer := make(chan *spectypes.Duty, executeDutiesCallSize)
	s.executeDuty = func(logger *zap.Logger, duty *spectypes.Duty) {
		logger.Debug("🏃 Executing duty", zap.Any("duty", duty))
		executeDutiesBuffer <- duty

		// Check if all expected duties have been received
		if len(executeDutiesBuffer) == executeDutiesCallSize {
			// Build the array of duties
			var duties []*spectypes.Duty
			for i := 0; i < executeDutiesCallSize; i++ {
				d := <-executeDutiesBuffer
				duties = append(duties, d)
			}

			// Send the array of duties to executeDutiesCall
			executeDutiesCall <- duties
		}
	}
}

func waitForDutiesFetch(t *testing.T, logger *zap.Logger, fetchDutiesCall chan struct{}, executeDutiesCall chan []*spectypes.Duty, timeout time.Duration) {
	select {
	case <-fetchDutiesCall:
		logger.Debug("duties fetched")
	case <-executeDutiesCall:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for duties to be fetched")
	}
}

func waitForNoAction(t *testing.T, logger *zap.Logger, fetchDutiesCall chan struct{}, executeDutiesCall chan []*spectypes.Duty, timeout time.Duration) {
	select {
	case <-fetchDutiesCall:
		require.FailNow(t, "unexpected duties call")
	case <-executeDutiesCall:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		// No action as expected.
	}
}

func waitForDutiesExecution(t *testing.T, logger *zap.Logger, fetchDutiesCall chan struct{}, executeDutiesCall chan []*spectypes.Duty, timeout time.Duration, expectedDuties []*spectypes.Duty) {
	select {
	case <-fetchDutiesCall:
		require.FailNow(t, "unexpected duties call")
	case duties := <-executeDutiesCall:
		logger.Debug("duties executed", zap.Any("duties", duties))
		logger.Debug("expected duties", zap.Any("duties", expectedDuties))
		require.Len(t, duties, len(expectedDuties))
		for _, e := range expectedDuties {
			found := false
			for _, d := range duties {
				if e.Type == d.Type && e.PubKey == d.PubKey && e.ValidatorIndex == d.ValidatorIndex && e.Slot == d.Slot {
					found = true
					break
				}
			}
			require.True(t, found)
		}

	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for duty to be executed")
	}
}

type SlotValue struct {
	mu   sync.Mutex
	slot phase0.Slot
}

func (sv *SlotValue) SetSlot(s phase0.Slot) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.slot = s
}

func (sv *SlotValue) GetSlot() phase0.Slot {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.slot
}

func TestScheduler_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
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
	mockTicker.EXPECT().Subscribe(gomock.Any()).Return(nil).Times(len(s.handlers) + 1)

	// setup mock duty handler expectations
	for _, mockDutyHandler := range s.handlers {
		mockDutyHandler.(*MockdutyHandler).EXPECT().Setup(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		mockDutyHandler.(*MockdutyHandler).EXPECT().HandleDuties(gomock.Any()).
			DoAndReturn(func(ctx context.Context) {
				<-ctx.Done()
			}).
			Times(1)
		mockDutyHandler.(*MockdutyHandler).EXPECT().Name().Times(1)
	}

	require.NoError(t, s.Start(ctx, logger))

	// Cancel the context and test that the scheduler stops.
	cancel()
	require.NoError(t, s.Wait())
}

func TestScheduler_Regression_IndiciesChangeStuck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, _ := context.WithCancel(context.Background())
	logger := logging.TestLogger(t)

	mockBeaconNode := mocks.NewMockBeaconNode(ctrl)
	mockValidatorController := mocks.NewMockValidatorController(ctrl)
	mockTicker := mockslotticker.NewMockTicker(ctrl)
	// create multiple mock duty handlers

	opts := &SchedulerOptions{
		Ctx:                 ctx,
		BeaconNode:          mockBeaconNode,
		Network:             networkconfig.TestNetwork,
		ValidatorController: mockValidatorController,
		Ticker:              mockTicker,
		IndicesChg:          make(chan struct{}),

		BuilderProposals: true,
	}

	s := NewScheduler(opts)

	// add multiple mock duty handlers
	s.handlers = []dutyHandler{NewValidatorRegistrationHandler()}
	mockBeaconNode.EXPECT().Events(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	mockTicker.EXPECT().Subscribe(gomock.Any()).Return(nil).Times(len(s.handlers) + 1)
	err := s.Start(ctx, logger)
	require.NoError(t, err)

	select {
	case s.indicesChg <- struct{}{}: // first time make fanout stuck
		select {
		case s.indicesChg <- struct{}{}: // second send should hang
			break
		default:
			t.Fatal("Channel is jammed")
		}
	}
}
