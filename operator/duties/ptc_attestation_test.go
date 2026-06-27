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

// captureExecutor records the duties handed to ExecuteDuties so a test can assert on them.
type captureExecutor struct {
	executed chan []*spectypes.ValidatorDuty
}

func (c *captureExecutor) ExecuteDuties(_ context.Context, duties []*spectypes.ValidatorDuty, _ time.Time) {
	c.executed <- duties
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
		Return([]*gloas.PTCDuty{{ValidatorIndex: idx, Slot: dutySlot}}, nil).
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
		Return([]*gloas.PTCDuty{
			{ValidatorIndex: selfIdx, Slot: dutySlot},
			{ValidatorIndex: otherIdx, Slot: dutySlot},
		}, nil)

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
		Return([]*gloas.PTCDuty{{ValidatorIndex: idx}}, nil).AnyTimes()

	store := dutystore.NewDuties[gloas.PTCDuty]()
	h := NewPTCAttestationHandler(store, false)
	h.logger = zap.NewNop()
	h.netCfg = netCfg
	h.validatorProvider = vp
	h.beaconNode = bn

	h.HandleInitialDuties(context.Background())

	require.True(t, store.IsEpochSet(netCfg.EstimatedCurrentEpoch()))
}

// A reorg or indices change drops the cached PTC duties so the next tick re-fetches them (SIP #94 §3).
func TestPTCAttestationHandler_invalidateDuties_clearsCache(t *testing.T) {
	store := dutystore.NewDuties[gloas.PTCDuty]()
	for _, epoch := range []phase0.Epoch{100, 101} {
		store.Set(epoch, []dutystore.StoreDuty[gloas.PTCDuty]{
			{Slot: 1, ValidatorIndex: 1, Duty: &gloas.PTCDuty{}},
		})
	}

	h := NewPTCAttestationHandler(store, false)
	h.logger = zap.NewNop()

	h.invalidateDuties("test")

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
