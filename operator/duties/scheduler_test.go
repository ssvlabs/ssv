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

func setupSchedulerAndMocks(t *testing.T, handler dutyHandler, slotToStart phase0.Slot) (
	*Scheduler,
	*mockSlotTicker,
	*zap.Logger,
	chan *spectypes.Duty,
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

	executeDutiesCall := make(chan *spectypes.Duty)
	opts := &SchedulerOptions{
		Ctx:                 ctx,
		BeaconNode:          mockBeaconNode,
		Network:             mockNetworkConfig,
		ValidatorController: mockValidatorController,
		ExecuteDuty: func(logger *zap.Logger, duty *spectypes.Duty) {
			logger.Debug("executing duty", zap.Any("duty", duty))
			executeDutiesCall <- duty
		},
		Ticker:           mockTicker,
		BuilderProposals: false,
	}

	s := NewScheduler(opts)
	s.indicesChg = make(chan bool)
	s.handlers = []dutyHandler{handler}

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

	for _, h := range s.handlers {
		if h.Name() == spectypes.BNRoleProposer.String() {
			continue
		}
		mockNetworkConfig.Beacon.(*mocknetwork.MockNetworkInfo).EXPECT().EstimatedCurrentSlot().Return(slotToStart).Times(1)
	}

	schedulerPool := pool.New().WithErrors().WithContext(ctx)
	schedulerReady := make(chan struct{})
	schedulerPool.Go(func(ctx context.Context) error {
		return s.Run(ctx, logger, schedulerReady)
	})
	<-schedulerReady

	return s, mockTicker, logger, executeDutiesCall, cancel, schedulerPool
}

func waitForDutiesFetch(t *testing.T, logger *zap.Logger, fetchDutiesCall chan struct{}, executeDutiesCall chan *spectypes.Duty, timeout time.Duration) {
	select {
	case <-fetchDutiesCall:
		//logger.Debug("duties fetched")
	case <-executeDutiesCall:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for duties to be fetched")
	}
}

func waitForNoAction(t *testing.T, logger *zap.Logger, fetchDutiesCall chan struct{}, executeDutiesCall chan *spectypes.Duty, timeout time.Duration) {
	select {
	case <-fetchDutiesCall:
		require.FailNow(t, "unexpected duties call")
	case <-executeDutiesCall:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		// No action as expected.
	}
}

func waitForDutyExecution(t *testing.T, logger *zap.Logger, fetchDutiesCall chan struct{}, executeDutiesCall chan *spectypes.Duty, timeout time.Duration) {
	select {
	case <-fetchDutiesCall:
		require.FailNow(t, "unexpected duties call")
	case <-executeDutiesCall:
		//logger.Debug("duty executed")
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
