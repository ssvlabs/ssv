package duties

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

func TestSchedulerScheduleBuilderBidPrefetch_ClearsPastPlans(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	logger := log.TestLogger(t)

	exec := NewMockExecutionClient(ctrl)
	baseCtx, cancelBaseCtx := context.WithCancel(context.Background())
	t.Cleanup(cancelBaseCtx)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = time.Hour
	beaconCfg.GenesisTime = time.Now().Add(-2 * beaconCfg.SlotDuration) // currentSlot ~= 2

	s := NewScheduler(logger, &SchedulerOptions{
		Ctx:                  baseCtx,
		ExecutionClient:      exec,
		BeaconConfig:         &beaconCfg,
		BuilderBidPrefetcher: &recordingPrefetcher{},
		PrefetchLeadTime:     time.Minute,
		SlotTickerProvider:   func() slotticker.SlotTicker { return NewMockSlotTicker(baseCtx) },
	})

	currentSlot := beaconCfg.EstimatedCurrentSlot()
	staleSlot := currentSlot - 1
	futureSlot := currentSlot + 1

	staleTimer := time.NewTimer(time.Hour)
	t.Cleanup(func() {
		staleTimer.Stop()
	})

	s.prefetchMu.Lock()
	s.prefetchPlans[staleSlot] = &slotPrefetchPlan{
		timer:   staleTimer,
		pubkeys: map[phase0.BLSPubKey]struct{}{{1}: {}},
	}
	s.prefetchMu.Unlock()

	s.ScheduleBuilderBidPrefetch([]*spectypes.ValidatorDuty{{
		Type:   spectypes.BNRoleProposer,
		Slot:   futureSlot,
		PubKey: phase0.BLSPubKey{2},
	}})

	if hasPrefetchPlan(s, staleSlot) {
		t.Fatalf("expected stale prefetch plan for slot %d to be cleared", staleSlot)
	}
	if !hasPrefetchPlan(s, futureSlot) {
		t.Fatalf("expected future prefetch plan for slot %d to be scheduled", futureSlot)
	}
}

func TestSchedulerScheduleBuilderBidPrefetch_ClearsPlansOnContextCancel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	logger := log.TestLogger(t)

	exec := NewMockExecutionClient(ctrl)
	baseCtx, cancelBaseCtx := context.WithCancel(context.Background())

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = time.Hour
	beaconCfg.GenesisTime = time.Now().Add(-beaconCfg.SlotDuration) // currentSlot ~= 1

	s := NewScheduler(logger, &SchedulerOptions{
		Ctx:                  baseCtx,
		ExecutionClient:      exec,
		BeaconConfig:         &beaconCfg,
		BuilderBidPrefetcher: &recordingPrefetcher{},
		PrefetchLeadTime:     time.Minute,
		SlotTickerProvider:   func() slotticker.SlotTicker { return NewMockSlotTicker(baseCtx) },
	})

	currentSlot := beaconCfg.EstimatedCurrentSlot()
	s.ScheduleBuilderBidPrefetch([]*spectypes.ValidatorDuty{{
		Type:   spectypes.BNRoleProposer,
		Slot:   currentSlot + 2,
		PubKey: phase0.BLSPubKey{3},
	}})

	if got := prefetchPlanCount(s); got != 1 {
		t.Fatalf("unexpected prefetch plan count before cancel: got %d want 1", got)
	}

	cancelBaseCtx()

	deadline := time.Now().Add(time.Second)
	for prefetchPlanCount(s) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for prefetch plans to be cleared")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasPrefetchPlan(s *Scheduler, slot phase0.Slot) bool {
	s.prefetchMu.Lock()
	defer s.prefetchMu.Unlock()

	_, ok := s.prefetchPlans[slot]
	return ok
}

func prefetchPlanCount(s *Scheduler) int {
	s.prefetchMu.Lock()
	defer s.prefetchMu.Unlock()

	return len(s.prefetchPlans)
}

var _ ExecutionClient = (*MockExecutionClient)(nil)
