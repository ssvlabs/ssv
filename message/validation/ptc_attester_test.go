package validation

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

// A PTC member is drawn from a beacon committee, and a validator sits on exactly one beacon committee
// per epoch → at most one PTC duty per epoch, plus a reorg margin → limit 2.
func TestDutyLimit_PTCAttester(t *testing.T) {
	mv := &messageValidator{netCfg: networkconfig.TestNetwork}
	msgID := ssvtestingutils.NewMsgID(spectypes.DomainType{}, make([]byte, 48), spectypes.RolePTCAttester)

	limit, ok := mv.dutyLimit(msgID, 0, nil)
	require.True(t, ok)
	require.Equal(t, uint64(2), limit)
}

// PTC fires at the slot's 75% cutoff (a current-slot duty): fine around its slot, late for a slot well behind.
func TestMessageLateness_PTCAttester(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}
	slot := phase0.Slot(1000)

	notLate := mv.messageLateness(slot, spectypes.RolePTCAttester, netCfg.SlotStartTime(slot))
	require.LessOrEqual(t, notLate, time.Duration(0))

	late := mv.messageLateness(slot, spectypes.RolePTCAttester, netCfg.SlotStartTime(slot+100))
	require.Greater(t, late, time.Duration(0))
}

// A PTC attestation message must reference a real PTC assignment for the validator once the slot's
// epoch is fetched; an unfetched epoch is tolerated (the duty fetch may be in flight).
func TestValidateBeaconDuty_PTCAttesterRequiresAssignment(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	const epoch = phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)
	slot := phase0.Slot(uint64(epoch)*netCfg.SlotsPerEpoch + 3)

	ds := dutystore.New()
	ds.PTC.Set(epoch, []dutystore.StoreDuty[gloas.PTCDuty]{
		{Slot: slot, ValidatorIndex: idx, Duty: &gloas.PTCDuty{Slot: slot, ValidatorIndex: idx}, InCommittee: true},
	})
	mv := &messageValidator{netCfg: netCfg, dutyStore: ds}

	indices := []phase0.ValidatorIndex{idx}
	// Assigned PTC slot → accepted.
	require.NoError(t, mv.validateBeaconDuty(spectypes.RolePTCAttester, slot, indices, false))
	// Same (fetched) epoch, unassigned slot → rejected.
	require.ErrorIs(t, mv.validateBeaconDuty(spectypes.RolePTCAttester, slot+1, indices, false), ErrNoDuty)
	// Unfetched epoch → tolerated.
	unfetched := phase0.Slot(uint64(epoch+10) * netCfg.SlotsPerEpoch)
	require.NoError(t, mv.validateBeaconDuty(spectypes.RolePTCAttester, unfetched, indices, false))
}
