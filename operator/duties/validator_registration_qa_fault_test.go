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
	"github.com/ssvlabs/ssv/qa/faults"
)

func TestGloasVRDeprecatedRespectsTheFault(t *testing.T) {
	// gloasVRDeprecated is the single seam both Gloas gates now call, so the fault is tested there
	// rather than through the whole handler.
	t.Run("honest node stops the heartbeat at the fork", func(t *testing.T) {
		require.True(t, gloasVRDeprecated(true))
		require.False(t, gloasVRDeprecated(false))
	})

	t.Run("vr-postfork keeps it running", func(t *testing.T) {
		faults.SetForTest(t, faults.VRPostFork)
		require.False(t, gloasVRDeprecated(true))
		require.False(t, gloasVRDeprecated(false))
	})
}

// vrCadenceShares returns two attesting self shares, indices 7 and 3 (deliberately unsorted).
func vrCadenceShares() []*types.SSVShare {
	mk := func(idx phase0.ValidatorIndex, pk byte) *types.SSVShare {
		return &types.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  idx,
				ValidatorPubKey: spectypes.ValidatorPK{pk},
			},
			ActivationEpoch: 0,
			// needed so that ActivationEpoch is taken into account by IsAttesting.
			Status: eth2apiv1.ValidatorStatePendingQueued,
		}
	}
	return []*types.SSVShare{mk(7, 7), mk(3, 3)}
}

// runVRProcessExecutionOverEpoch calls processExecution for every slot of epoch and returns every
// duty it emitted.
func runVRProcessExecutionOverEpoch(t *testing.T, epoch phase0.Epoch) []*spectypes.ValidatorDuty {
	t.Helper()
	netCfg := networkconfig.TestNetworkWithGloas(vrCadenceGloasEpoch)

	ctrl := gomock.NewController(t)
	vp := NewMockValidatorProvider(ctrl)
	vp.EXPECT().SelfValidators().Return(vrCadenceShares()).AnyTimes()

	executed := make(chan []*spectypes.ValidatorDuty, netCfg.SlotsPerEpoch)
	h := NewValidatorRegistrationHandler(nil)
	h.logger = zap.NewNop()
	h.netCfg = netCfg
	h.validatorProvider = vp
	h.dutiesExecutor = &captureExecutor{executed: executed}

	first := netCfg.FirstSlotAtEpoch(epoch)
	for s := first; s < first+phase0.Slot(netCfg.SlotsPerEpoch); s++ {
		h.processExecution(context.Background(), epoch, s)
	}
	close(executed)

	var all []*spectypes.ValidatorDuty
	for d := range executed {
		all = append(all, d...)
	}
	return all
}

const vrCadenceGloasEpoch = phase0.Epoch(100)

// TestVRPostForkCadence pins decision D4: under vr-postfork, at Gloas epochs the periodic schedule
// registers exactly ONE validator (the lowest-index attesting self share) once per epoch, so the
// value lands inside a 2-epoch M3 window instead of waiting up to 320 slots for a residue. Pre-fork
// epochs and FAULT=none keep the stock 320-slot cadence.
func TestVRPostForkCadence(t *testing.T) {
	t.Run("fault on, Gloas epoch: one duty, lowest index, once per epoch", func(t *testing.T) {
		faults.SetForTest(t, faults.VRPostFork)
		for _, epoch := range []phase0.Epoch{vrCadenceGloasEpoch, vrCadenceGloasEpoch + 1} {
			duties := runVRProcessExecutionOverEpoch(t, epoch)
			require.Len(t, duties, 1, "epoch %d", epoch)
			require.Equal(t, phase0.ValidatorIndex(3), duties[0].ValidatorIndex)
			require.Equal(t, spectypes.BNRoleValidatorRegistration, duties[0].Type)
			require.EqualValues(t, 3, uint64(duties[0].Slot)%32, "fires on the validator's residue within the epoch")
		}
	})

	t.Run("fault off, Gloas epoch: nothing", func(t *testing.T) {
		require.Empty(t, runVRProcessExecutionOverEpoch(t, vrCadenceGloasEpoch))
	})

	t.Run("pre-fork epoch: the fault leaves the 320-slot cadence unchanged", func(t *testing.T) {
		// Epoch 10 covers slots 320..351: indices 3 and 7 both hit their 320-slot residue here.
		honest := runVRProcessExecutionOverEpoch(t, 10)
		require.Len(t, honest, 2)

		faults.SetForTest(t, faults.VRPostFork)
		require.Equal(t, honest, runVRProcessExecutionOverEpoch(t, 10))

		// Epoch 11 (slots 352..383) hits no residue, with or without the fault.
		require.Empty(t, runVRProcessExecutionOverEpoch(t, 11))
	})
}
