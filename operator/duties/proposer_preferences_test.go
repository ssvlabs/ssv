package duties

import (
	"context"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

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
			proposalSlot := phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch) // a slot in the Gloas fork epoch

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
