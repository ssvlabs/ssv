package duties

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"github.com/sourcegraph/conc/pool"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/operator/slotticker"
	mockslotticker "github.com/ssvlabs/ssv/operator/slotticker/mocks"
)

const (
	baseDuration            = 100 * time.Millisecond
	slotDuration            = 15 * baseDuration
	timeout                 = 20 * baseDuration
	noActionTimeout         = 7 * baseDuration
	clockError              = baseDuration
	testBlockPropagateDelay = baseDuration
	// testSlotTickerTriggerDelay is used to wait for the slot ticker to be triggered
	// in the attester, sync committee, and cluster handlers.
	// This ensures that no attester duties are fetched before the cluster ticker is triggered,
	// preventing a scenario where the cluster handler executes duties in the same slot as the attester fetching them.
	testSlotTickerTriggerDelay = baseDuration
	testEpochsPerSCPeriod      = 4
	testSlotsPerEpoch          = 12
)

type MockSlotTicker interface {
	Next() <-chan time.Time
	Slot() phase0.Slot
	Subscribe() chan phase0.Slot
}

type mockSlotTicker struct {
	slotChan chan phase0.Slot
	timeChan chan time.Time
	slot     phase0.Slot
	mu       sync.Mutex
}

func NewMockSlotTicker() MockSlotTicker {
	ticker := &mockSlotTicker{
		slotChan: make(chan phase0.Slot),
		timeChan: make(chan time.Time),
	}
	ticker.start()
	return ticker
}

func (m *mockSlotTicker) start() {
	go func() {
		for slot := range m.slotChan {
			m.mu.Lock()
			m.slot = slot
			m.mu.Unlock()
			m.timeChan <- time.Now()
		}
	}()
}

func (m *mockSlotTicker) Next() <-chan time.Time {
	return m.timeChan
}

func (m *mockSlotTicker) Slot() phase0.Slot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slot
}

func (m *mockSlotTicker) Subscribe() chan phase0.Slot {
	return m.slotChan
}

type mockSlotTickerService struct {
	event.Feed
}

func waitForSlotN(beaconCfg *networkconfig.Beacon, slots phase0.Slot) {
	waitUntil := beaconCfg.GenesisTime.Add(beaconCfg.SlotDuration * time.Duration(slots))
	time.Sleep(time.Until(waitUntil))
}

func setupSchedulerAndMocks(
	ctx context.Context,
	t *testing.T,
	handlers []dutyHandler,
) (
	*Scheduler,
	*mockSlotTickerService,
	*pool.ContextPool,
) {
	return setupSchedulerAndMocksWithParams(ctx, t, handlers, time.Now(), slotDuration)
}

func setupSchedulerAndMocksWithStartSlot(
	ctx context.Context,
	t *testing.T,
	handlers []dutyHandler,
	startSlot phase0.Slot,
) (
	*Scheduler,
	*mockSlotTickerService,
	*pool.ContextPool,
) {
	genesisTime := time.Now().Add(-slotDuration * time.Duration(startSlot))
	return setupSchedulerAndMocksWithParams(ctx, t, handlers, genesisTime, slotDuration)
}

func setupSchedulerAndMocksWithParams(
	ctx context.Context,
	t *testing.T,
	handlers []dutyHandler,
	genesisTime time.Time,
	slotDuration time.Duration,
) (
	*Scheduler,
	*mockSlotTickerService,
	*pool.ContextPool,
) {
	ctrl := gomock.NewController(t)

	logger := log.TestLogger(t)

	mockBeaconNode := NewMockBeaconNode(ctrl)
	mockExecutionClient := NewMockExecutionClient(ctrl)
	mockValidatorProvider := NewMockValidatorProvider(ctrl)
	mockValidatorController := NewMockValidatorController(ctrl)
	mockDutyExecutor := NewMockDutyExecutor(ctrl)
	mockSlotService := &mockSlotTickerService{}

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = slotDuration
	beaconCfg.GenesisTime = genesisTime
	beaconCfg.EpochsPerSyncCommitteePeriod = testEpochsPerSCPeriod
	beaconCfg.SlotsPerEpoch = testSlotsPerEpoch

	opts := &SchedulerOptions{
		Ctx:                 ctx,
		BeaconNode:          mockBeaconNode,
		ExecutionClient:     mockExecutionClient,
		BeaconConfig:        &beaconCfg,
		ValidatorProvider:   mockValidatorProvider,
		ValidatorController: mockValidatorController,
		DutyExecutor:        mockDutyExecutor,
		SlotTickerProvider: func() slotticker.SlotTicker {
			ticker := NewMockSlotTicker()
			mockSlotService.Subscribe(ticker.Subscribe())
			return ticker
		},
	}

	s := NewScheduler(logger, opts)
	s.blockPropagateDelay = testBlockPropagateDelay
	s.indicesChg = make(chan struct{})
	s.handlers = handlers

	mockBeaconNode.EXPECT().SubscribeToHeadEvents(ctx, "duty_scheduler", gomock.Any()).Return(nil)

	// Create a pool to wait for the scheduler to finish.
	schedulerPool := pool.New().WithErrors().WithContext(ctx)

	return s, mockSlotService, schedulerPool
}

func startScheduler(ctx context.Context, t *testing.T, s *Scheduler, schedulerPool *pool.ContextPool) {
	err := s.Start(ctx)
	require.NoError(t, err)

	schedulerPool.Go(func(ctx context.Context) error {
		return s.Wait()
	})
}

func setExecuteDutyFunc(s *Scheduler, executeDutiesCall chan []*spectypes.ValidatorDuty, executeDutiesCallSize int) {
	executeDutiesBuffer := make(chan *spectypes.ValidatorDuty, executeDutiesCallSize)

	s.dutyExecutor.(*MockDutyExecutor).EXPECT().ExecuteDuty(gomock.Any(), gomock.Any()).Times(executeDutiesCallSize).DoAndReturn(
		func(_ context.Context, duty *spectypes.ValidatorDuty) error {
			s.logger.Debug("🏃 Executing duty", zap.Any("duty", duty))

			executeDutiesBuffer <- duty

			// Once all expected duties have been received, build an array of duties and send it
			// over executeDutiesCall channel.
			if len(executeDutiesBuffer) == executeDutiesCallSize {
				var duties []*spectypes.ValidatorDuty
				for i := 0; i < executeDutiesCallSize; i++ {
					d := <-executeDutiesBuffer
					duties = append(duties, d)
				}
				executeDutiesCall <- duties
			}
			return nil
		},
	)
}

func setExecuteDutyFuncs(s *Scheduler, executeDutiesCall chan committeeDutiesMap, executeDutiesCallSize int) {
	executeDutiesBuffer := make(chan *committeeDuty, executeDutiesCallSize)

	// We are not super interested in checking the exact number of ExecuteDuty calls, hence allow
	// AnyTimes of these calls here.
	s.dutyExecutor.(*MockDutyExecutor).EXPECT().ExecuteDuty(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, duty *spectypes.ValidatorDuty) error {
			s.logger.Debug("🏃 Executing duty", zap.Any("duty", duty))
			return nil
		},
	)

	s.dutyExecutor.(*MockDutyExecutor).EXPECT().ExecuteCommitteeDuty(gomock.Any(), gomock.Any(), gomock.Any()).Times(executeDutiesCallSize).DoAndReturn(
		func(ctx context.Context, committeeID spectypes.CommitteeID, duty *spectypes.CommitteeDuty) {
			s.logger.Debug("🏃 Executing committee duty", zap.Any("duty", duty))
			executeDutiesBuffer <- &committeeDuty{id: committeeID, duty: duty}

			// Check if all expected duties have been received
			if len(executeDutiesBuffer) == executeDutiesCallSize {
				// Build the array of duties
				duties := make(committeeDutiesMap)
				for i := 0; i < executeDutiesCallSize; i++ {
					d := <-executeDutiesBuffer

					if _, ok := duties[d.id]; !ok {
						duties[d.id] = d
					}
				}

				// Send the array of duties to executeDutiesCall
				executeDutiesCall <- duties
			}
		},
	)
}

func waitForDutiesFetch(
	t *testing.T,
	fetchDutiesCall chan struct{},
	executeDutiesCall chan []*spectypes.ValidatorDuty,
	timeout time.Duration,
) {
	logger := log.TestLogger(t)

	select {
	case <-fetchDutiesCall:
		logger.Debug("duties fetched")
	case <-executeDutiesCall:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for duties to be fetched")
	}
}

func waitForNoAction(
	t *testing.T,
	fetchDutiesCall chan struct{},
	executeDutiesCall chan []*spectypes.ValidatorDuty,
	timeout time.Duration,
) {
	select {
	case <-fetchDutiesCall:
		require.FailNow(t, "unexpected duties call")
	case <-executeDutiesCall:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		// No action as expected.
	}
}

func waitForDutiesExecution(
	t *testing.T,
	fetchDutiesCall chan struct{},
	executeDutiesCall chan []*spectypes.ValidatorDuty,
	timeout time.Duration,
	expectedDuties []*spectypes.ValidatorDuty,
) {
	logger := log.TestLogger(t)

	select {
	case <-fetchDutiesCall:
		require.FailNow(t, "unexpected fetch-duties call")
	case duties := <-executeDutiesCall:
		logger.Debug("duties executed", zap.Any("duties", duties))
		logger.Debug("duties expected", zap.Any("duties", expectedDuties))
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
		// Wait a tiny bit more to make sure no more duties are coming (mock expectations will catch that
		// if that's the case).
		time.Sleep(1 * time.Millisecond)
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for duty to be executed")
	}
}

func waitForDutiesFetchCommittee(
	t *testing.T,
	fetchDutiesCall chan struct{},
	executeDutiesCall chan committeeDutiesMap,
	timeout time.Duration,
) {
	select {
	case <-fetchDutiesCall:
		break
	case <-executeDutiesCall:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for duties to be fetched")
	}
}

func waitForNoActionCommittee(
	t *testing.T,
	fetchDutiesCall chan struct{},
	executeDutiesCall chan committeeDutiesMap,
	timeout time.Duration,
) {
	select {
	case <-fetchDutiesCall:
		require.FailNow(t, "unexpected duties call")
	case <-executeDutiesCall:
		require.FailNow(t, "unexpected execute duty call")
	case <-time.After(timeout):
		// No action as expected.
	}
}

func waitForDutiesExecutionCommittee(
	t *testing.T,
	fetchDutiesCall chan struct{},
	executeDutiesCall chan committeeDutiesMap,
	timeout time.Duration,
	expectedDuties committeeDutiesMap,
) {
	select {
	case <-fetchDutiesCall:
		require.FailNow(t, "unexpected duties call")
	case actualDuties := <-executeDutiesCall:
		require.Len(t, actualDuties, len(expectedDuties))
		for eCommitteeID, eCommDuty := range expectedDuties {
			aCommDuty, ok := actualDuties[eCommitteeID]
			if !ok {
				require.FailNow(t, "missing cluster id")
			}
			require.Len(t, aCommDuty.duty.ValidatorDuties, len(eCommDuty.duty.ValidatorDuties))

			for _, e := range eCommDuty.duty.ValidatorDuties {
				found := false
				for _, d := range aCommDuty.duty.ValidatorDuties {
					if e.Type == d.Type && e.PubKey == d.PubKey && e.ValidatorIndex == d.ValidatorIndex && e.Slot == d.Slot {
						found = true
						break
					}
				}
				require.True(t, found)
			}
		}

	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for duty to be executed")
	}
}

// SafeValue is a generic type that can hold any type specified by T.
type SafeValue[T any] struct {
	mu sync.Mutex
	v  T
}

// Set sets the value of SafeValue to the specified value of type T.
func (sv *SafeValue[T]) Set(v T) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.v = v
}

// Get returns the current value of SafeValue of type T.
func (sv *SafeValue[T]) Get() T {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.v
}

func TestScheduler_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	logger := log.TestLogger(t)

	mockBeaconNode := NewMockBeaconNode(ctrl)
	mockValidatorProvider := NewMockValidatorProvider(ctrl)
	mockTicker := mockslotticker.NewMockSlotTicker(ctrl)
	// create multiple mock duty handlers
	mockDutyHandler1 := NewMockdutyHandler(ctrl)
	mockDutyHandler2 := NewMockdutyHandler(ctrl)

	mockDutyHandler1.EXPECT().HandleInitialDuties(gomock.Any()).AnyTimes()
	mockDutyHandler2.EXPECT().HandleInitialDuties(gomock.Any()).AnyTimes()

	opts := &SchedulerOptions{
		Ctx:               ctx,
		BeaconNode:        mockBeaconNode,
		BeaconConfig:      networkconfig.TestNetwork.Beacon,
		ValidatorProvider: mockValidatorProvider,
		SlotTickerProvider: func() slotticker.SlotTicker {
			return mockTicker
		},
	}

	s := NewScheduler(logger, opts)
	// add multiple mock duty handlers
	s.handlers = []dutyHandler{mockDutyHandler1, mockDutyHandler2}

	mockBeaconNode.EXPECT().SubscribeToHeadEvents(ctx, "duty_scheduler", gomock.Any()).Return(nil)
	mockTicker.EXPECT().Next().Return(nil).AnyTimes()

	// setup mock duty handler expectations
	for _, mockDutyHandler := range s.handlers {
		mockDutyHandler.(*MockdutyHandler).EXPECT().Setup(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		mockDutyHandler.(*MockdutyHandler).EXPECT().HandleDuties(gomock.Any()).
			DoAndReturn(func(ctx context.Context) {
				<-ctx.Done()
			}).
			Times(1)
		mockDutyHandler.(*MockdutyHandler).EXPECT().Name().Times(1)
	}

	require.NoError(t, s.Start(ctx))

	// Cancel the context and test that the scheduler stops.
	cancel()
	require.NoError(t, s.Wait())
}

func TestScheduler_Regression_IndicesChangeStuck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	logger := log.TestLogger(t)

	mockBeaconNode := NewMockBeaconNode(ctrl)
	mockValidatorProvider := NewMockValidatorProvider(ctrl)
	mockTicker := mockslotticker.NewMockSlotTicker(ctrl)
	// create multiple mock duty handlers

	opts := &SchedulerOptions{
		Ctx:               ctx,
		BeaconNode:        mockBeaconNode,
		BeaconConfig:      networkconfig.TestNetwork.Beacon,
		ValidatorProvider: mockValidatorProvider,
		SlotTickerProvider: func() slotticker.SlotTicker {
			return mockTicker
		},
		IndicesChg: make(chan struct{}),
	}

	s := NewScheduler(logger, opts)

	// add multiple mock duty handlers
	s.handlers = []dutyHandler{NewValidatorRegistrationHandler(nil)}
	mockBeaconNode.EXPECT().SubscribeToHeadEvents(ctx, "duty_scheduler", gomock.Any()).Return(nil)
	mockTicker.EXPECT().Next().Return(nil).AnyTimes()
	err := s.Start(ctx)
	require.NoError(t, err)

	s.indicesChg <- struct{}{} // first time make fanout stuck
	select {
	case s.indicesChg <- struct{}{}: // second send should hang
		break
	case <-time.After(timeout):
		t.Fatal("Channel is jammed")
	}
}
