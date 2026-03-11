package duties

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

type chanPrefetcher struct {
	ch chan prefetchCall
}

func (p *chanPrefetcher) PrefetchBid(_ context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) {
	p.ch <- prefetchCall{slot: slot, parentHash: parentHash, pubkey: pubkey}
}

func TestSchedulerScheduleBuilderBidPrefetch_DoesNotFireEarly(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	logger := log.TestLogger(t)

	exec := NewMockExecutionClient(ctrl)

	baseCtx, cancelBaseCtx := context.WithCancel(context.Background())
	t.Cleanup(cancelBaseCtx)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = 200 * time.Millisecond
	beaconCfg.GenesisTime = time.Now().Add(-beaconCfg.SlotDuration) // currentSlot == 1

	currentSlot := beaconCfg.EstimatedCurrentSlot()
	targetSlot := currentSlot + 1

	header := &ethtypes.Header{Number: big.NewInt(1), Time: uint64(time.Now().Unix())}
	exec.EXPECT().HeaderByNumber(gomock.Any(), (*big.Int)(nil)).Return(header, nil).Times(1)

	ch := make(chan prefetchCall, 1)
	prefetcher := &chanPrefetcher{ch: ch}

	s := NewScheduler(logger, &SchedulerOptions{
		Ctx:                       baseCtx,
		ExecutionClient:           exec,
		BeaconConfig:              &beaconCfg,
		BuilderBidPrefetcher:      prefetcher,
		PrefetchParentHashTimeout: 200 * time.Millisecond,
		PrefetchLeadTime:          150 * time.Millisecond, // should fire ~50ms after scheduling
		SlotTickerProvider:        func() slotticker.SlotTicker { return NewMockSlotTicker(baseCtx) },
	})

	duty := &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleProposer,
		Slot:           targetSlot,
		PubKey:         phase0.BLSPubKey{1},
		ValidatorIndex: 1,
	}

	s.ScheduleBuilderBidPrefetch([]*spectypes.ValidatorDuty{duty})

	select {
	case <-ch:
		t.Fatalf("prefetch fired too early")
	case <-time.After(20 * time.Millisecond):
	}

	select {
	case <-ch:
	case <-time.After(750 * time.Millisecond):
		t.Fatalf("timed out waiting for scheduled prefetch")
	}
}

func TestSchedulerScheduleBuilderBidPrefetch_MergesPubkeysAndFetchesHeadOnce(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	logger := log.TestLogger(t)

	exec := NewMockExecutionClient(ctrl)

	baseCtx, cancelBaseCtx := context.WithCancel(context.Background())
	t.Cleanup(cancelBaseCtx)

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.SlotDuration = 200 * time.Millisecond
	beaconCfg.GenesisTime = time.Now().Add(-beaconCfg.SlotDuration) // currentSlot == 1

	currentSlot := beaconCfg.EstimatedCurrentSlot()
	targetSlot := currentSlot + 1

	header := &ethtypes.Header{Number: big.NewInt(2), Time: uint64(time.Now().Unix())}
	exec.EXPECT().HeaderByNumber(gomock.Any(), (*big.Int)(nil)).Return(header, nil).Times(1)

	ch := make(chan prefetchCall, 2)
	prefetcher := &chanPrefetcher{ch: ch}

	s := NewScheduler(logger, &SchedulerOptions{
		Ctx:                       baseCtx,
		ExecutionClient:           exec,
		BeaconConfig:              &beaconCfg,
		BuilderBidPrefetcher:      prefetcher,
		PrefetchParentHashTimeout: 200 * time.Millisecond,
		PrefetchLeadTime:          150 * time.Millisecond,
		SlotTickerProvider:        func() slotticker.SlotTicker { return NewMockSlotTicker(baseCtx) },
	})

	d1 := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: targetSlot, PubKey: phase0.BLSPubKey{1}, ValidatorIndex: 1}
	d2 := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: targetSlot, PubKey: phase0.BLSPubKey{2}, ValidatorIndex: 2}

	s.ScheduleBuilderBidPrefetch([]*spectypes.ValidatorDuty{d1})
	s.ScheduleBuilderBidPrefetch([]*spectypes.ValidatorDuty{d2})

	gotPubkeys := make(map[phase0.BLSPubKey]struct{})

	deadline := time.Now().Add(750 * time.Millisecond)
	for len(gotPubkeys) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for scheduled prefetch calls")
		}
		select {
		case call := <-ch:
			if call.slot != targetSlot {
				t.Fatalf("unexpected slot: got %d want %d", call.slot, targetSlot)
			}
			gotPubkeys[call.pubkey] = struct{}{}
		case <-time.After(10 * time.Millisecond):
		}
	}

	if _, ok := gotPubkeys[phase0.BLSPubKey{1}]; !ok {
		t.Fatalf("missing pubkey 1")
	}
	if _, ok := gotPubkeys[phase0.BLSPubKey{2}]; !ok {
		t.Fatalf("missing pubkey 2")
	}
}
