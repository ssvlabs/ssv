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

// fetchDuties caches an epoch's duties on first fetch and short-circuits on repeat — the Times(1)
// expectations on both mocks fail if the second call re-fetches.
func TestPTCAttestationHandler_fetchDuties_cachesPerEpoch(t *testing.T) {
	ctrl := gomock.NewController(t)

	epoch := phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)
	dutySlot := phase0.Slot(60)
	pk := phase0.BLSPubKey{1, 2, 3}

	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().SelfParticipatingValidators(epoch).
		Return([]*types.SSVShare{{Share: spectypes.Share{ValidatorIndex: idx, ValidatorPubKey: spectypes.ValidatorPK(pk)}}}).
		Times(1)

	bn := NewMockBeaconNode(ctrl)
	bn.EXPECT().PayloadAttestationDuties(gomock.Any(), epoch, []phase0.ValidatorIndex{idx}).
		Return([]*gloas.PTCDuty{{PubKey: pk, ValidatorIndex: idx, Slot: dutySlot}}, nil).
		Times(1)

	h := NewPTCAttestationHandler()
	h.logger = zap.NewNop()
	h.validatorProvider = vp
	h.beaconNode = bn

	h.fetchDuties(context.Background(), epoch)
	h.fetchDuties(context.Background(), epoch)

	require.Contains(t, h.duties, epoch)
	require.Len(t, h.duties[epoch][dutySlot], 1)
	require.Equal(t, spectypes.BNRolePTCAttester, h.duties[epoch][dutySlot][0].Type)
	require.Equal(t, idx, h.duties[epoch][dutySlot][0].ValidatorIndex)
}

// evictOutdated drops only epochs strictly before the current one.
func TestPTCAttestationHandler_evictOutdated(t *testing.T) {
	h := NewPTCAttestationHandler()
	for _, e := range []phase0.Epoch{4, 5, 6} {
		h.duties[e] = map[phase0.Slot][]*spectypes.ValidatorDuty{}
	}

	h.evictOutdated(5)

	require.NotContains(t, h.duties, phase0.Epoch(4))
	require.Contains(t, h.duties, phase0.Epoch(5))
	require.Contains(t, h.duties, phase0.Epoch(6))
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
		h := NewPTCAttestationHandler()
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
