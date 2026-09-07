package duties

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// captureExecutor records the duties handed to ExecuteDuties (and, when a deadlines channel is set,
// each call's duty deadline) so a test can assert on them.
type captureExecutor struct {
	executed  chan []*spectypes.ValidatorDuty
	deadlines chan time.Time
}

func (c *captureExecutor) ExecuteDuties(_ context.Context, duties []*spectypes.ValidatorDuty, deadline time.Time) {
	c.executed <- duties
	if c.deadlines != nil {
		c.deadlines <- deadline
	}
}

func (c *captureExecutor) ExecuteCommitteeDuties(context.Context, committeeDutiesMap, time.Time) {}

// fetchDuties records an epoch's duties once and short-circuits on repeat — the Times(1)
// expectations fail if the second call re-fetches.
func TestPTCAttestationHandler_fetchDuties_cachesPerEpoch(t *testing.T) {
	ctrl := gomock.NewController(t)

	epoch := phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)
	dutySlot := phase0.Slot(60)

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().Validators().Return([]*types.SSVShare{activeShare(idx)}).Times(1)
	vp.EXPECT().SelfParticipatingValidators(epoch).Return([]*types.SSVShare{activeShare(idx)}).Times(1)

	bn := NewMockBeaconNode(ctrl)
	bn.EXPECT().PayloadAttestationDuties(gomock.Any(), epoch, []phase0.ValidatorIndex{idx}).
		Return(&gloas.PTCDuties{Duties: []*gloas.PTCDuty{{ValidatorIndex: idx, Slot: dutySlot}}}, nil).
		Times(1)

	store := dutystore.NewDuties[gloas.PTCDuty]()
	h := NewPTCAttestationHandler(store, false)
	h.logger = zap.NewNop()
	h.netCfg = networkconfig.TestNetwork
	h.validatorProvider = vp
	h.beaconNode = bn

	h.fetchDuties(context.Background(), epoch)
	h.fetchDuties(context.Background(), epoch)

	require.True(t, store.IsEpochSet(epoch))
	require.NotNil(t, store.ValidatorDuty(epoch, dutySlot, idx))
}

// fetchDuties records every participating validator's duty so the message validator can check
// assignments, marking only this node's own InCommittee (executable).
func TestPTCAttestationHandler_fetchDuties_recordsAllMarksSelf(t *testing.T) {
	ctrl := gomock.NewController(t)

	epoch := phase0.Epoch(5)
	selfIdx := phase0.ValidatorIndex(7)
	otherIdx := phase0.ValidatorIndex(8)
	dutySlot := phase0.Slot(60)

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().Validators().Return([]*types.SSVShare{activeShare(selfIdx), activeShare(otherIdx)})
	vp.EXPECT().SelfParticipatingValidators(epoch).Return([]*types.SSVShare{activeShare(selfIdx)})

	bn := NewMockBeaconNode(ctrl)
	bn.EXPECT().PayloadAttestationDuties(gomock.Any(), epoch, gomock.Any()).
		Return(&gloas.PTCDuties{Duties: []*gloas.PTCDuty{
			{ValidatorIndex: selfIdx, Slot: dutySlot},
			{ValidatorIndex: otherIdx, Slot: dutySlot},
		}}, nil)

	store := dutystore.NewDuties[gloas.PTCDuty]()
	h := NewPTCAttestationHandler(store, false)
	h.logger = zap.NewNop()
	h.netCfg = networkconfig.TestNetwork
	h.validatorProvider = vp
	h.beaconNode = bn

	h.fetchDuties(context.Background(), epoch)

	// Both validators are recorded so the message validator can check assignments...
	require.NotNil(t, store.ValidatorDuty(epoch, dutySlot, selfIdx))
	require.NotNil(t, store.ValidatorDuty(epoch, dutySlot, otherIdx))
	// ...but only this node's own duty is executable.
	executable := store.CommitteeSlotDuties(epoch, dutySlot)
	require.Len(t, executable, 1)
	require.Equal(t, selfIdx, executable[0].ValidatorIndex)
}

// HandleInitialDuties pre-fetches the current epoch on startup, so the store is populated before the
// first tick.
func TestPTCAttestationHandler_HandleInitialDuties_prefetchesCurrentEpoch(t *testing.T) {
	ctrl := gomock.NewController(t)

	netCfg := networkconfig.TestNetworkWithGloas(0) // Gloas from genesis.
	idx := phase0.ValidatorIndex(7)

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().Validators().Return([]*types.SSVShare{activeShare(idx)}).AnyTimes()
	vp.EXPECT().SelfParticipatingValidators(gomock.Any()).Return([]*types.SSVShare{activeShare(idx)}).AnyTimes()

	bn := NewMockBeaconNode(ctrl)
	bn.EXPECT().PayloadAttestationDuties(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&gloas.PTCDuties{Duties: []*gloas.PTCDuty{{ValidatorIndex: idx}}}, nil).AnyTimes()

	store := dutystore.NewDuties[gloas.PTCDuty]()
	h := NewPTCAttestationHandler(store, false)
	h.logger = zap.NewNop()
	h.netCfg = netCfg
	h.validatorProvider = vp
	h.beaconNode = bn

	h.HandleInitialDuties(context.Background())

	require.True(t, store.IsEpochSet(netCfg.EstimatedCurrentEpoch()))
}

// An indices change drops the cached PTC duties so the next tick re-fetches them (SIP #94 §3).
func TestPTCAttestationHandler_invalidateDuties_clearsCache(t *testing.T) {
	store := dutystore.NewDuties[gloas.PTCDuty]()
	for _, epoch := range []phase0.Epoch{100, 101} {
		store.Set(epoch, []dutystore.StoreDuty[gloas.PTCDuty]{
			{Slot: 1, ValidatorIndex: 1, Duty: &gloas.PTCDuty{}},
		})
	}

	h := NewPTCAttestationHandler(store, false)
	h.logger = zap.NewNop()

	h.invalidateDuties()

	require.False(t, store.IsEpochSet(100))
	require.False(t, store.IsEpochSet(101))
}

// scheduleExecution fires the duty at the 75%-of-slot cutoff, not before.
func TestPTCAttestationHandler_scheduleExecution_firesAtCutoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		beaconCfg := *networkconfig.TestNetwork.Beacon
		beaconCfg.GenesisTime = time.Now()
		beaconCfg.SlotDuration = time.Second
		beaconCfg.SlotsPerEpoch = testSlotsPerEpoch
		netCfg := *networkconfig.TestNetwork
		netCfg.Beacon = &beaconCfg

		executed := make(chan []*spectypes.ValidatorDuty, 1)
		h := NewPTCAttestationHandler(dutystore.NewDuties[gloas.PTCDuty](), false)
		h.logger = zap.NewNop()
		h.netCfg = &netCfg
		h.dutiesExecutor = &captureExecutor{executed: executed}

		slot := phase0.Slot(3)
		duties := []*spectypes.ValidatorDuty{{Type: spectypes.BNRolePTCAttester, Slot: slot}}
		h.scheduleExecution(context.Background(), slot, duties)

		cutoff := netCfg.PayloadAttestationCutoff(slot)

		// Just shy of the cutoff: nothing executed yet.
		time.Sleep(time.Until(cutoff) - time.Millisecond)
		synctest.Wait()
		select {
		case <-executed:
			t.Fatal("duty executed before the 75% cutoff")
		default:
		}

		// Crossing the cutoff triggers execution with the scheduled duties.
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()
		select {
		case got := <-executed:
			require.Equal(t, duties, got)
		default:
			t.Fatal("duty not executed at the cutoff")
		}
	})
}

// gloasTestNetwork returns a test network with Gloas from forkEpoch whose clock places the given slot
// at the present, so slot-bounded fetch deadlines lie in the future.
func gloasTestNetwork(forkEpoch phase0.Epoch, now phase0.Slot) *networkconfig.Network {
	netCfg := networkconfig.TestNetworkWithGloas(forkEpoch)
	netCfg.GenesisTime = time.Now().Add(-time.Duration(now) * netCfg.SlotDuration)
	return netCfg
}

// newTestPTCHandler wires a handler over the given network, store and beacon mock, with validators
// selfIdx (this node's own) and others (recorded for message validation only).
func newTestPTCHandler(ctrl *gomock.Controller, netCfg *networkconfig.Network, store *dutystore.Duties[gloas.PTCDuty], bn *MockBeaconNode, selfIdx phase0.ValidatorIndex, others ...phase0.ValidatorIndex) *PTCAttestationHandler {
	all := make([]*types.SSVShare, 0, 1+len(others))
	all = append(all, activeShare(selfIdx))
	for _, idx := range others {
		all = append(all, activeShare(idx))
	}
	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().Validators().Return(all).AnyTimes()
	vp.EXPECT().SelfParticipatingValidators(gomock.Any()).Return([]*types.SSVShare{activeShare(selfIdx)}).AnyTimes()

	h := NewPTCAttestationHandler(store, false)
	h.logger = zap.NewNop()
	h.netCfg = netCfg
	h.validatorProvider = vp
	h.beaconNode = bn
	return h
}

// The look-ahead also runs in the epoch before the Gloas fork, so the fork epoch's duties are in the
// store before its first slot rather than fetched against the cutoff (ssv#3027).
func TestPTCAttestationHandler_handleTick_lookaheadAcrossFork(t *testing.T) {
	const forkEpoch = phase0.Epoch(10)
	idx := phase0.ValidatorIndex(7)
	firstForkSlot := networkconfig.TestNetwork.FirstSlotAtEpoch(forkEpoch)

	t.Run("second half of the pre-fork epoch fetches the fork epoch once", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		slot := firstForkSlot - 8
		netCfg := gloasTestNetwork(forkEpoch, slot)

		bn := NewMockBeaconNode(ctrl)
		bn.EXPECT().PayloadAttestationDuties(gomock.Any(), forkEpoch, []phase0.ValidatorIndex{idx}).
			Return(&gloas.PTCDuties{Duties: []*gloas.PTCDuty{{ValidatorIndex: idx, Slot: firstForkSlot}}}, nil).
			Times(1)

		store := dutystore.NewDuties[gloas.PTCDuty]()
		h := newTestPTCHandler(ctrl, netCfg, store, bn, idx)

		h.handleTick(context.Background(), slot)
		h.handleTick(context.Background(), slot+1) // cached: no second fetch

		require.True(t, store.IsEpochSet(forkEpoch))
		require.False(t, store.IsEpochSet(forkEpoch-1), "PTC is a Gloas-only duty")
		require.Contains(t, h.lookahead, forkEpoch, "awaits reconciliation at the fork's first tick")
	})

	t.Run("first half of the pre-fork epoch fetches nothing", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		slot := firstForkSlot - 30
		netCfg := gloasTestNetwork(forkEpoch, slot)

		store := dutystore.NewDuties[gloas.PTCDuty]()
		h := newTestPTCHandler(ctrl, netCfg, store, NewMockBeaconNode(ctrl), idx) // any beacon call fails the test

		h.handleTick(context.Background(), slot)

		require.False(t, store.IsEpochSet(forkEpoch))
	})
}

// HandleInitialDuties pre-fetches the fork epoch when the node starts in the second half of the epoch
// before the fork.
func TestPTCAttestationHandler_HandleInitialDuties_prefetchesForkEpochBeforeFork(t *testing.T) {
	ctrl := gomock.NewController(t)

	const forkEpoch = phase0.Epoch(10)
	idx := phase0.ValidatorIndex(7)
	firstForkSlot := networkconfig.TestNetwork.FirstSlotAtEpoch(forkEpoch)
	netCfg := gloasTestNetwork(forkEpoch, firstForkSlot-8)

	bn := NewMockBeaconNode(ctrl)
	bn.EXPECT().PayloadAttestationDuties(gomock.Any(), forkEpoch, gomock.Any()).
		Return(&gloas.PTCDuties{Duties: []*gloas.PTCDuty{{ValidatorIndex: idx, Slot: firstForkSlot}}}, nil).
		Times(1)

	store := dutystore.NewDuties[gloas.PTCDuty]()
	h := newTestPTCHandler(ctrl, netCfg, store, bn, idx)

	h.HandleInitialDuties(context.Background())

	require.True(t, store.IsEpochSet(forkEpoch))
	require.False(t, store.IsEpochSet(forkEpoch-1))
}

// An epoch cached by the look-ahead is re-fetched at its first tick and the settled answer replaces
// the cached one, for the slot being scheduled included (ssv#3027).
func TestPTCAttestationHandler_handleTick_reconcilesLookaheadAtFirstTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		selfIdx, otherIdx := phase0.ValidatorIndex(7), phase0.ValidatorIndex(8)
		epoch := phase0.Epoch(6)
		firstSlot := networkconfig.TestNetwork.FirstSlotAtEpoch(epoch)
		lookaheadSlot := firstSlot - 8
		netCfg := gloasTestNetwork(0, lookaheadSlot)

		dependentRoot := phase0.Root{0xaa}
		bn := NewMockBeaconNode(ctrl)
		// Look-ahead view: our duty in the epoch's second slot.
		bn.EXPECT().PayloadAttestationDuties(gomock.Any(), epoch, gomock.Any()).
			Return(&gloas.PTCDuties{DependentRoot: dependentRoot, Duties: []*gloas.PTCDuty{{ValidatorIndex: selfIdx, Slot: firstSlot + 1}}}, nil).
			Times(1)
		// Settled view at the first tick, under the same dependent_root: our duty moved to the first
		// slot, another validator's appeared.
		bn.EXPECT().PayloadAttestationDuties(gomock.Any(), epoch, gomock.Any()).
			Return(&gloas.PTCDuties{DependentRoot: dependentRoot, Duties: []*gloas.PTCDuty{
				{ValidatorIndex: selfIdx, Slot: firstSlot},
				{ValidatorIndex: otherIdx, Slot: firstSlot + 1},
			}}, nil).
			Times(1)

		store := dutystore.NewDuties[gloas.PTCDuty]()
		h := newTestPTCHandler(ctrl, netCfg, store, bn, selfIdx, otherIdx)
		executed := make(chan []*spectypes.ValidatorDuty, 1)
		h.dutiesExecutor = &captureExecutor{executed: executed}

		// Mid-epoch tick: the look-ahead fills the next epoch. The current epoch is pre-filled so
		// only the look-ahead reaches the beacon node.
		store.Set(epoch-1, nil)
		h.handleTick(context.Background(), lookaheadSlot)
		require.NotNil(t, store.ValidatorDuty(epoch, firstSlot+1, selfIdx))
		require.Equal(t, dependentRoot, h.lookahead[epoch])

		// First tick of the epoch: reconciled before scheduling, so the moved duty is what fires.
		time.Sleep(time.Until(netCfg.SlotStartTime(firstSlot)))
		h.handleTick(context.Background(), firstSlot)

		require.Nil(t, store.ValidatorDuty(epoch, firstSlot+1, selfIdx), "look-ahead assignment replaced")
		require.NotNil(t, store.ValidatorDuty(epoch, firstSlot, selfIdx))
		require.NotNil(t, store.ValidatorDuty(epoch, firstSlot+1, otherIdx))
		require.NotContains(t, h.lookahead, epoch)

		time.Sleep(time.Until(netCfg.PayloadAttestationCutoff(firstSlot)) + time.Millisecond)
		synctest.Wait()
		select {
		case got := <-executed:
			require.Len(t, got, 1)
			require.Equal(t, selfIdx, got[0].ValidatorIndex)
			require.Equal(t, firstSlot, got[0].Slot)
		default:
			t.Fatal("reconciled duty not executed at the cutoff")
		}

		// Later ticks in the epoch serve the settled view without further fetches.
		time.Sleep(time.Until(netCfg.SlotStartTime(firstSlot + 1)))
		h.handleTick(context.Background(), firstSlot+1)
	})
}

// A failed reconciliation keeps the look-ahead view in use and is retried at the next tick.
func TestPTCAttestationHandler_handleTick_reconcileFailureKeepsLookahead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		idx := phase0.ValidatorIndex(7)
		epoch := phase0.Epoch(6)
		firstSlot := networkconfig.TestNetwork.FirstSlotAtEpoch(epoch)
		lookaheadSlot := firstSlot - 8
		netCfg := gloasTestNetwork(0, lookaheadSlot)
		lookaheadDuties := &gloas.PTCDuties{Duties: []*gloas.PTCDuty{{ValidatorIndex: idx, Slot: firstSlot + 5}}}

		bn := NewMockBeaconNode(ctrl)
		bn.EXPECT().PayloadAttestationDuties(gomock.Any(), epoch, gomock.Any()).Return(lookaheadDuties, nil).Times(1)
		bn.EXPECT().PayloadAttestationDuties(gomock.Any(), epoch, gomock.Any()).Return(nil, context.DeadlineExceeded).Times(1)
		bn.EXPECT().PayloadAttestationDuties(gomock.Any(), epoch, gomock.Any()).Return(lookaheadDuties, nil).Times(1)

		store := dutystore.NewDuties[gloas.PTCDuty]()
		store.Set(epoch-1, nil)
		h := newTestPTCHandler(ctrl, netCfg, store, bn, idx)

		h.handleTick(context.Background(), lookaheadSlot)

		// The first tick's reconciliation fails: the look-ahead view stays and the epoch stays pending.
		time.Sleep(time.Until(netCfg.SlotStartTime(firstSlot)))
		h.handleTick(context.Background(), firstSlot)
		require.NotNil(t, store.ValidatorDuty(epoch, firstSlot+5, idx))
		require.Contains(t, h.lookahead, epoch)

		// The next tick retries and settles.
		time.Sleep(time.Until(netCfg.SlotStartTime(firstSlot + 1)))
		h.handleTick(context.Background(), firstSlot+1)
		require.NotNil(t, store.ValidatorDuty(epoch, firstSlot+5, idx))
		require.NotContains(t, h.lookahead, epoch)
	})
}

// Invalidation forgets pending look-aheads along with the cache: the re-fetch that follows decides
// afresh whether an epoch is a look-ahead.
func TestPTCAttestationHandler_invalidateDuties_forgetsLookahead(t *testing.T) {
	h := NewPTCAttestationHandler(dutystore.NewDuties[gloas.PTCDuty](), false)
	h.logger = zap.NewNop()
	h.lookahead[7] = phase0.Root{1}

	h.invalidateDuties()

	require.Empty(t, h.lookahead)
}

func TestDiffDuties(t *testing.T) {
	duty := func(slot phase0.Slot, idx phase0.ValidatorIndex) dutystore.StoreDuty[gloas.PTCDuty] {
		return dutystore.StoreDuty[gloas.PTCDuty]{Slot: slot, ValidatorIndex: idx, Duty: &gloas.PTCDuty{}}
	}
	before := []dutystore.StoreDuty[gloas.PTCDuty]{duty(1, 7), duty(2, 8), duty(3, 9)}
	after := []dutystore.StoreDuty[gloas.PTCDuty]{duty(1, 7), duty(2, 9), duty(4, 8)}

	added, removed := diffDuties(before, after)
	require.Equal(t, 2, added)   // (2, 9) and (4, 8)
	require.Equal(t, 2, removed) // (2, 8) and (3, 9)

	added, removed = diffDuties(before, before)
	require.Zero(t, added)
	require.Zero(t, removed)
}

// A reorg drops only the epoch whose dependent_root changed: the "previous" root covers the current
// epoch, the "current" root the next one, here still a pending look-ahead.
func TestPTCAttestationHandler_handleReorg_dropsAffectedEpochOnly(t *testing.T) {
	epoch := phase0.Epoch(6)
	netCfg := gloasTestNetwork(0, networkconfig.TestNetwork.FirstSlotAtEpoch(epoch)+20)

	newHandler := func() (*PTCAttestationHandler, *dutystore.Duties[gloas.PTCDuty]) {
		store := dutystore.NewDuties[gloas.PTCDuty]()
		store.Set(epoch, nil)
		store.Set(epoch+1, nil)
		h := NewPTCAttestationHandler(store, false)
		h.logger = zap.NewNop()
		h.netCfg = netCfg
		h.lookahead[epoch+1] = phase0.Root{1}
		return h, store
	}

	t.Run("previous root changed: the current epoch is re-fetched", func(t *testing.T) {
		h, store := newHandler()
		h.handleReorg(ReorgEvent{PreviousDutyDependentRootChanged: true})

		require.False(t, store.IsEpochSet(epoch))
		require.True(t, store.IsEpochSet(epoch+1))
		require.Contains(t, h.lookahead, epoch+1)
	})

	t.Run("current root changed: the next epoch is re-fetched", func(t *testing.T) {
		h, store := newHandler()
		h.handleReorg(ReorgEvent{CurrentDutyDependentRootChanged: true})

		require.True(t, store.IsEpochSet(epoch))
		require.False(t, store.IsEpochSet(epoch+1))
		require.NotContains(t, h.lookahead, epoch+1)
	})
}
