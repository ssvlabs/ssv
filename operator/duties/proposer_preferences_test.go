package duties

import (
	"context"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// emitForEpoch fetches an epoch's proposer assignments once and emits one proposer-preferences duty
// per assignment; a repeat call short-circuits (the Times(1) expectations fail if it re-fetches).
func TestProposerPreferencesHandler_emitForEpoch_emitsAndCachesPerEpoch(t *testing.T) {
	ctrl := gomock.NewController(t)

	epoch := phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)
	proposalSlot := phase0.Slot(60)
	currentSlot := phase0.Slot(40)
	pk := phase0.BLSPubKey{1, 2, 3}

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().SelfParticipatingValidators(epoch).
		Return([]*types.SSVShare{{Share: spectypes.Share{ValidatorIndex: idx, ValidatorPubKey: spectypes.ValidatorPK(pk)}}}).
		Times(1)

	bn := NewMockBeaconNode(ctrl)
	bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), epoch).Return(phase0.Root{0xaa}, nil).Times(1)
	bn.EXPECT().ProposerDuties(gomock.Any(), epoch, []phase0.ValidatorIndex{idx}).
		Return([]*eth2apiv1.ProposerDuty{{PubKey: pk, ValidatorIndex: idx, Slot: proposalSlot}}, nil).
		Times(1)

	executed := make(chan []*spectypes.ValidatorDuty, 1)
	h := NewProposerPreferencesHandler()
	h.logger = zap.NewNop()
	h.netCfg = networkconfig.TestNetwork
	h.validatorProvider = vp
	h.beaconNode = bn
	h.dutiesExecutor = &captureExecutor{executed: executed}

	h.emitForEpoch(context.Background(), epoch, currentSlot, false)
	h.emitForEpoch(context.Background(), epoch, currentSlot, false) // cached: must not re-fetch or re-emit

	require.Contains(t, h.emitted, epoch)

	require.Len(t, executed, 1)
	got := <-executed
	require.Len(t, got, 1)
	require.Equal(t, spectypes.BNRoleProposerPreferences, got[0].Type)
	require.Equal(t, idx, got[0].ValidatorIndex)
	require.Equal(t, proposalSlot, got[0].Slot) // duty.Slot is the proposal slot
}

// With no local validators for the epoch, nothing is emitted and the epoch is left unprocessed so a
// later tick (once validators load) retries. beaconNode/dutiesExecutor are nil here: reaching either
// would panic, asserting the early return.
func TestProposerPreferencesHandler_emitForEpoch_noLocalValidators(t *testing.T) {
	ctrl := gomock.NewController(t)
	epoch := phase0.Epoch(5)

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().SelfParticipatingValidators(epoch).Return(nil).Times(1)

	h := NewProposerPreferencesHandler()
	h.logger = zap.NewNop()
	h.validatorProvider = vp

	h.emitForEpoch(context.Background(), epoch, phase0.Slot(40), false)

	require.NotContains(t, h.emitted, epoch)
}

// When the beacon node reports no local proposals for the epoch, it's marked processed (no retry) and
// nothing is emitted.
func TestProposerPreferencesHandler_emitForEpoch_noProposals(t *testing.T) {
	ctrl := gomock.NewController(t)
	epoch := phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().SelfParticipatingValidators(epoch).
		Return([]*types.SSVShare{{Share: spectypes.Share{ValidatorIndex: idx}}}).Times(1)

	bn := NewMockBeaconNode(ctrl)
	bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), epoch).Return(phase0.Root{0xaa}, nil).Times(1)
	bn.EXPECT().ProposerDuties(gomock.Any(), epoch, []phase0.ValidatorIndex{idx}).
		Return([]*eth2apiv1.ProposerDuty{}, nil).Times(1)

	executed := make(chan []*spectypes.ValidatorDuty, 1)
	h := NewProposerPreferencesHandler()
	h.logger = zap.NewNop()
	h.validatorProvider = vp
	h.beaconNode = bn
	h.dutiesExecutor = &captureExecutor{executed: executed}

	h.emitForEpoch(context.Background(), epoch, phase0.Slot(40), false)

	require.Contains(t, h.emitted, epoch)
	require.Len(t, executed, 0)
}

// emitForEpoch skips assignments whose proposal slot has already been reached (the preference is
// moot and peers would reject its partials as late) and bounds each remaining duty by its own
// proposal slot's end — §5 convergence legitimately spans the slots up to it.
func TestProposerPreferencesHandler_emitForEpoch_skipsReachedSlotsAndBoundsPerDuty(t *testing.T) {
	ctrl := gomock.NewController(t)

	epoch := phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)
	currentSlot := phase0.Slot(40)
	pk := phase0.BLSPubKey{1, 2, 3}

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().SelfParticipatingValidators(epoch).
		Return([]*types.SSVShare{{Share: spectypes.Share{ValidatorIndex: idx, ValidatorPubKey: spectypes.ValidatorPK(pk)}}}).Times(1)

	bn := NewMockBeaconNode(ctrl)
	bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), epoch).Return(phase0.Root{0xaa}, nil).Times(1)
	bn.EXPECT().ProposerDuties(gomock.Any(), epoch, []phase0.ValidatorIndex{idx}).
		Return([]*eth2apiv1.ProposerDuty{
			{PubKey: pk, ValidatorIndex: idx, Slot: currentSlot - 1}, // passed: skipped
			{PubKey: pk, ValidatorIndex: idx, Slot: currentSlot},     // reached: skipped
			{PubKey: pk, ValidatorIndex: idx, Slot: currentSlot + 5}, // upcoming: emitted
		}, nil).Times(1)

	executed := make(chan []*spectypes.ValidatorDuty, 3)
	deadlines := make(chan time.Time, 3)
	h := NewProposerPreferencesHandler()
	h.logger = zap.NewNop()
	h.netCfg = networkconfig.TestNetwork
	h.validatorProvider = vp
	h.beaconNode = bn
	h.dutiesExecutor = &captureExecutor{executed: executed, deadlines: deadlines}

	h.emitForEpoch(context.Background(), epoch, currentSlot, false)

	require.Len(t, executed, 1, "only the upcoming assignment is emitted")
	got := <-executed
	require.Len(t, got, 1)
	require.Equal(t, currentSlot+5, got[0].Slot)
	require.Equal(t, networkconfig.TestNetwork.SlotStartTime(currentSlot+5+1), <-deadlines,
		"each duty's execution window runs to the end of its own proposal slot")
}

// The first Gloas tick forces a one-time lookahead recheck: a boundary epoch emitted pre-fork under
// a dependent_root that shifts at the fork transition is re-emitted under the fresh root; later
// Gloas ticks don't recheck again (the pinned mock call order fails on any extra fetch).
func TestProposerPreferencesHandler_firstGloasTickRechecksBoundaryEpoch(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)

	ctrl := gomock.NewController(t)
	idx := phase0.ValidatorIndex(7)
	pk := phase0.BLSPubKey{1, 2, 3}
	proposalSlot := phase0.Slot(uint64(gloasEpoch)*netCfg.SlotsPerEpoch) + 10
	rootA, rootB := phase0.Root{0xaa}, phase0.Root{0xbb}

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().SelfParticipatingValidators(phase0.Epoch(gloasEpoch)).
		Return([]*types.SSVShare{{Share: spectypes.Share{ValidatorIndex: idx, ValidatorPubKey: spectypes.ValidatorPK(pk)}}}).AnyTimes()

	duty := []*eth2apiv1.ProposerDuty{{PubKey: pk, ValidatorIndex: idx, Slot: proposalSlot}}
	bn := NewMockBeaconNode(ctrl)
	gomock.InOrder(
		bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), phase0.Epoch(gloasEpoch)).Return(rootA, nil), // pre-fork window emit
		bn.EXPECT().ProposerDuties(gomock.Any(), phase0.Epoch(gloasEpoch), []phase0.ValidatorIndex{idx}).Return(duty, nil),
		bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), phase0.Epoch(gloasEpoch)).Return(rootB, nil), // fork-tick recheck: root shifted
		bn.EXPECT().ProposerDuties(gomock.Any(), phase0.Epoch(gloasEpoch), []phase0.ValidatorIndex{idx}).Return(duty, nil),
	)

	executed := make(chan []*spectypes.ValidatorDuty, 2)
	h := NewProposerPreferencesHandler()
	h.logger = zap.NewNop()
	h.netCfg = netCfg
	h.validatorProvider = vp
	h.beaconNode = bn
	h.dutiesExecutor = &captureExecutor{executed: executed}

	preForkSlot := phase0.Slot(uint64(gloasEpoch-1) * netCfg.SlotsPerEpoch)
	forkSlot := phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch)

	h.emitForTick(context.Background(), preForkSlot) // pre-fork window: emits under rootA
	require.Equal(t, rootA, h.emitted[phase0.Epoch(gloasEpoch)])

	h.emitForTick(context.Background(), forkSlot) // first Gloas tick: forced recheck re-emits under rootB
	require.Equal(t, rootB, h.emitted[phase0.Epoch(gloasEpoch)])

	h.emitForTick(context.Background(), forkSlot+1) // second Gloas tick: no recheck, no re-fetch

	require.Len(t, executed, 2, "pre-fork emit and the fork-tick re-emit only")
}

// Ticks within the post-indices-change grace neither emit nor consume a pending reorg recheck (or
// the one-time fork recheck); the first post-grace tick proceeds normally.
func TestProposerPreferencesHandler_emitGraceAfterIndicesChange(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)

	ctrl := gomock.NewController(t)
	vp := NewMockValidatorProvider(ctrl)

	h := NewProposerPreferencesHandler()
	h.logger = zap.NewNop()
	h.netCfg = netCfg
	h.validatorProvider = vp
	h.beaconNode = NewMockBeaconNode(ctrl) // no expectations: any fetch during the grace fails the test
	h.dutiesExecutor = &captureExecutor{executed: make(chan []*spectypes.ValidatorDuty, 1)}

	slot := phase0.Slot(uint64(gloasEpoch)*netCfg.SlotsPerEpoch) + 3
	h.emitAfterSlot = slot + indicesChangeEmitGraceSlots
	h.recheckLookahead = true

	h.emitForTick(context.Background(), slot) // grace: skipped entirely
	h.emitForTick(context.Background(), slot+1)
	require.True(t, h.recheckLookahead, "a pending recheck must survive the grace")
	require.False(t, h.gloasForkRechecked, "the one-time fork recheck must not be consumed during the grace")

	// The first post-grace tick proceeds (and consumes the pending recheck); no local validators, so
	// it stops before touching the beacon node.
	vp.EXPECT().SelfParticipatingValidators(phase0.Epoch(gloasEpoch)).Return(nil).Times(1)
	h.emitForTick(context.Background(), slot+indicesChangeEmitGraceSlots)
	require.False(t, h.recheckLookahead, "the pending recheck is consumed by the first post-grace tick")
	require.True(t, h.gloasForkRechecked)
}

// evictOutdated drops only epochs strictly before the current one.
func TestProposerPreferencesHandler_evictOutdated(t *testing.T) {
	h := NewProposerPreferencesHandler()
	for _, e := range []phase0.Epoch{4, 5, 6} {
		h.emitted[e] = phase0.Root{}
	}

	h.evictOutdated(5)

	require.NotContains(t, h.emitted, phase0.Epoch(4))
	require.Contains(t, h.emitted, phase0.Epoch(5))
	require.Contains(t, h.emitted, phase0.Epoch(6))
}

// emitForTick emits the first Gloas epoch's preferences both in steady state (a slot in that epoch)
// and pre-fork (a slot in the epoch immediately before the fork).
func TestProposerPreferencesHandler_emitForTick(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)

	tt := []struct {
		name string
		slot phase0.Slot
	}{
		{"pre-fork window emits the first Gloas epoch", phase0.Slot(uint64(gloasEpoch-1) * netCfg.SlotsPerEpoch)},
		{"steady state emits the current Gloas epoch", phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch)},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			idx := phase0.ValidatorIndex(7)
			pk := phase0.BLSPubKey{1, 2, 3}
			// A still-upcoming slot in the Gloas fork epoch: the steady-state tick sits on the epoch's
			// first slot, and an assignment at the tick slot itself is filtered as already reached.
			proposalSlot := phase0.Slot(uint64(gloasEpoch)*netCfg.SlotsPerEpoch) + 1

			vp := NewMockValidatorProvider(ctrl)
			vp.EXPECT().SelfParticipatingValidators(phase0.Epoch(gloasEpoch)).
				Return([]*types.SSVShare{{Share: spectypes.Share{ValidatorIndex: idx, ValidatorPubKey: spectypes.ValidatorPK(pk)}}}).Times(1)

			bn := NewMockBeaconNode(ctrl)
			bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), phase0.Epoch(gloasEpoch)).Return(phase0.Root{0xaa}, nil).Times(1)
			bn.EXPECT().ProposerDuties(gomock.Any(), phase0.Epoch(gloasEpoch), []phase0.ValidatorIndex{idx}).
				Return([]*eth2apiv1.ProposerDuty{{PubKey: pk, ValidatorIndex: idx, Slot: proposalSlot}}, nil).Times(1)

			executed := make(chan []*spectypes.ValidatorDuty, 1)
			h := NewProposerPreferencesHandler()
			h.logger = zap.NewNop()
			h.netCfg = netCfg
			h.validatorProvider = vp
			h.beaconNode = bn
			h.dutiesExecutor = &captureExecutor{executed: executed}

			h.emitForTick(context.Background(), tc.slot)

			require.Len(t, executed, 1)
			got := <-executed
			require.Len(t, got, 1)
			require.Equal(t, spectypes.BNRoleProposerPreferences, got[0].Type)
			require.Equal(t, proposalSlot, got[0].Slot)
		})
	}
}

// A post-reorg recheck re-emits an epoch's preferences only when its dependent_root actually changed:
// an unchanged root is skipped (re-emitting it would just duplicate the preference, SIP #94 §5), while
// a changed root re-emits.
func TestProposerPreferencesHandler_recheckReEmitsOnlyOnDependentRootChange(t *testing.T) {
	ctrl := gomock.NewController(t)

	epoch := phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)
	proposalSlot := phase0.Slot(60)
	currentSlot := phase0.Slot(40)
	pk := phase0.BLSPubKey{1, 2, 3}
	rootA := phase0.Root{0xaa}
	rootB := phase0.Root{0xbb}

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().SelfParticipatingValidators(epoch).
		Return([]*types.SSVShare{{Share: spectypes.Share{ValidatorIndex: idx, ValidatorPubKey: spectypes.ValidatorPK(pk)}}}).
		AnyTimes()

	duty := []*eth2apiv1.ProposerDuty{{PubKey: pk, ValidatorIndex: idx, Slot: proposalSlot}}
	bn := NewMockBeaconNode(ctrl)
	// The first emit and the changed-root recheck each fetch duties; the unchanged-root recheck must not.
	gomock.InOrder(
		bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), epoch).Return(rootA, nil), // first emit
		bn.EXPECT().ProposerDuties(gomock.Any(), epoch, []phase0.ValidatorIndex{idx}).Return(duty, nil),
		bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), epoch).Return(rootA, nil), // recheck, unchanged → skip
		bn.EXPECT().ProposerDutiesDependentRoot(gomock.Any(), epoch).Return(rootB, nil), // recheck, changed → re-emit
		bn.EXPECT().ProposerDuties(gomock.Any(), epoch, []phase0.ValidatorIndex{idx}).Return(duty, nil),
	)

	executed := make(chan []*spectypes.ValidatorDuty, 2)
	h := NewProposerPreferencesHandler()
	h.logger = zap.NewNop()
	h.netCfg = networkconfig.TestNetwork
	h.validatorProvider = vp
	h.beaconNode = bn
	h.dutiesExecutor = &captureExecutor{executed: executed}

	h.emitForEpoch(context.Background(), epoch, currentSlot, false) // first emit → root A
	require.Equal(t, rootA, h.emitted[epoch])

	h.emitForEpoch(context.Background(), epoch, currentSlot, true) // recheck, unchanged → no re-emit
	h.emitForEpoch(context.Background(), epoch, currentSlot, true) // recheck, changed → re-emit
	require.Equal(t, rootB, h.emitted[epoch])

	require.Len(t, executed, 2) // first emit + changed-root re-emit only
}
