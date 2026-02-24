package duties

import (
	"context"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

type recordingPrefetcher struct {
	calls int32
	ch    chan prefetchCall
}

type prefetchCall struct {
	slot       phase0.Slot
	parentHash phase0.Hash32
	pubkey     phase0.BLSPubKey
}

func (p *recordingPrefetcher) PrefetchBid(_ context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) {
	atomic.AddInt32(&p.calls, 1)
	if p.ch != nil {
		p.ch <- prefetchCall{slot: slot, parentHash: parentHash, pubkey: pubkey}
	}
}

func TestSchedulerPrefetchesBuilderBidsForCurrentSlotProposerDuties(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	logger := log.TestLogger(t)
	exec := NewMockExecutionClient(ctrl)
	dutyExec := NewMockDutyExecutor(ctrl)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = 12 * time.Second
	// Set genesis such that EstimatedCurrentSlot() == 1.
	beaconCfg.GenesisTime = time.Now().Add(-beaconCfg.SlotDuration)

	currentSlot := beaconCfg.EstimatedCurrentSlot()
	if currentSlot != 1 {
		t.Fatalf("unexpected current slot: got %d want %d", currentSlot, 1)
	}

	header := &ethtypes.Header{
		Number: big.NewInt(1),
		Time:   uint64(time.Now().Unix()),
	}
	exec.EXPECT().HeaderByNumber(gomock.Any(), (*big.Int)(nil)).Return(header, nil).Times(1)

	prefetchCh := make(chan prefetchCall, 2)
	prefetcher := &recordingPrefetcher{ch: prefetchCh}

	dutyCalled := make(chan struct{})
	dutyExec.EXPECT().ExecuteDuty(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Do(func(_ context.Context, _ *zap.Logger, _ *spectypes.ValidatorDuty) {
		close(dutyCalled)
	})

	s := NewScheduler(logger, &SchedulerOptions{
		Ctx:                       context.Background(),
		ExecutionClient:           exec,
		BeaconConfig:              &beaconCfg,
		BuilderBidPrefetcher:      prefetcher,
		PrefetchParentHashTimeout: 200 * time.Millisecond,
		DutyExecutor:              dutyExec,
		SlotTickerProvider:        func() slotticker.SlotTicker { return NewMockSlotTicker() },
	})

	pubkey := phase0.BLSPubKey{2}
	duty := &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleProposer,
		Slot:           currentSlot,
		PubKey:         pubkey,
		ValidatorIndex: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	s.ExecuteDuties(ctx, []*spectypes.ValidatorDuty{duty})

	select {
	case <-dutyCalled:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for ExecuteDuty")
	}

	var expectedParentHash phase0.Hash32
	h := header.Hash()
	copy(expectedParentHash[:], h[:])

	select {
	case got := <-prefetchCh:
		if got.slot != currentSlot {
			t.Fatalf("unexpected slot: got %d want %d", got.slot, currentSlot)
		}
		if got.pubkey != pubkey {
			t.Fatalf("unexpected pubkey")
		}
		if got.parentHash != expectedParentHash {
			t.Fatalf("unexpected parent hash")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for PrefetchBid")
	}
}

func TestSchedulerDoesNotPrefetchForNextSlotDuties(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	logger := log.TestLogger(t)
	exec := NewMockExecutionClient(ctrl)
	dutyExec := NewMockDutyExecutor(ctrl)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = 12 * time.Second
	beaconCfg.GenesisTime = time.Now().Add(-beaconCfg.SlotDuration)

	// Ensure HeaderByNumber is not called.
	exec.EXPECT().HeaderByNumber(gomock.Any(), gomock.Any()).Times(0)

	prefetchCh := make(chan prefetchCall, 1)
	prefetcher := &recordingPrefetcher{ch: prefetchCh}

	dutyCalled := make(chan struct{})
	dutyExec.EXPECT().ExecuteDuty(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Do(func(_ context.Context, _ *zap.Logger, _ *spectypes.ValidatorDuty) {
		close(dutyCalled)
	})

	s := NewScheduler(logger, &SchedulerOptions{
		Ctx:                       context.Background(),
		ExecutionClient:           exec,
		BeaconConfig:              &beaconCfg,
		BuilderBidPrefetcher:      prefetcher,
		PrefetchParentHashTimeout: 200 * time.Millisecond,
		DutyExecutor:              dutyExec,
		SlotTickerProvider:        func() slotticker.SlotTicker { return NewMockSlotTicker() },
	})

	currentSlot := beaconCfg.EstimatedCurrentSlot()
	duty := &spectypes.ValidatorDuty{
		Type: spectypes.BNRoleProposer,
		Slot: currentSlot + 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	s.ExecuteDuties(ctx, []*spectypes.ValidatorDuty{duty})

	select {
	case <-dutyCalled:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for ExecuteDuty")
	}

	select {
	case <-prefetchCh:
		t.Fatalf("unexpected prefetch call")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSchedulerPrefetchExecutionClientErrorDoesNotBlockDutyExecution(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	logger := log.TestLogger(t)
	exec := NewMockExecutionClient(ctrl)
	dutyExec := NewMockDutyExecutor(ctrl)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = 12 * time.Second
	beaconCfg.GenesisTime = time.Now().Add(-beaconCfg.SlotDuration)

	exec.EXPECT().HeaderByNumber(gomock.Any(), (*big.Int)(nil)).Return((*ethtypes.Header)(nil), context.DeadlineExceeded).Times(1)

	prefetchCh := make(chan prefetchCall, 1)
	prefetcher := &recordingPrefetcher{ch: prefetchCh}

	dutyCalled := make(chan struct{})
	dutyExec.EXPECT().ExecuteDuty(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Do(func(_ context.Context, _ *zap.Logger, _ *spectypes.ValidatorDuty) {
		close(dutyCalled)
	})

	s := NewScheduler(logger, &SchedulerOptions{
		Ctx:                       context.Background(),
		ExecutionClient:           exec,
		BeaconConfig:              &beaconCfg,
		BuilderBidPrefetcher:      prefetcher,
		PrefetchParentHashTimeout: 50 * time.Millisecond,
		DutyExecutor:              dutyExec,
		SlotTickerProvider:        func() slotticker.SlotTicker { return NewMockSlotTicker() },
	})

	currentSlot := beaconCfg.EstimatedCurrentSlot()
	duty := &spectypes.ValidatorDuty{
		Type: spectypes.BNRoleProposer,
		Slot: currentSlot,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	s.ExecuteDuties(ctx, []*spectypes.ValidatorDuty{duty})

	select {
	case <-dutyCalled:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for ExecuteDuty")
	}

	select {
	case <-prefetchCh:
		t.Fatalf("unexpected prefetch call")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSchedulerPrefetchesOncePerBatch(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	logger := log.TestLogger(t)
	exec := NewMockExecutionClient(ctrl)
	dutyExec := NewMockDutyExecutor(ctrl)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = 12 * time.Second
	beaconCfg.GenesisTime = time.Now().Add(-beaconCfg.SlotDuration)

	header := &ethtypes.Header{
		Number: big.NewInt(2),
		Time:   uint64(time.Now().Unix()),
	}
	exec.EXPECT().HeaderByNumber(gomock.Any(), (*big.Int)(nil)).Return(header, nil).Times(1)

	prefetchCh := make(chan prefetchCall, 2)
	prefetcher := &recordingPrefetcher{ch: prefetchCh}

	var dutyCalls int32
	dutyExec.EXPECT().ExecuteDuty(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).Do(func(_ context.Context, _ *zap.Logger, _ *spectypes.ValidatorDuty) {
		atomic.AddInt32(&dutyCalls, 1)
	})

	s := NewScheduler(logger, &SchedulerOptions{
		Ctx:                       context.Background(),
		ExecutionClient:           exec,
		BeaconConfig:              &beaconCfg,
		BuilderBidPrefetcher:      prefetcher,
		PrefetchParentHashTimeout: 200 * time.Millisecond,
		DutyExecutor:              dutyExec,
		SlotTickerProvider:        func() slotticker.SlotTicker { return NewMockSlotTicker() },
	})

	currentSlot := beaconCfg.EstimatedCurrentSlot()
	d1 := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: currentSlot, PubKey: phase0.BLSPubKey{1}}
	d2 := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: currentSlot, PubKey: phase0.BLSPubKey{2}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	s.ExecuteDuties(ctx, []*spectypes.ValidatorDuty{d1, d2})

	// Expect two prefetch calls (one per duty) and only one HeaderByNumber.
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&prefetcher.calls) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for prefetch calls")
		}
		time.Sleep(5 * time.Millisecond)
	}

	deadline = time.Now().Add(time.Second)
	for atomic.LoadInt32(&dutyCalls) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for duty calls")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
