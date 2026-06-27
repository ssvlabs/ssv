package validation

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
)

// A PTC member signs at most one payload attestation per slot → at most SlotsPerEpoch per epoch.
func TestDutyLimit_PTCAttester(t *testing.T) {
	mv := &messageValidator{netCfg: networkconfig.TestNetwork}
	msgID := spectypes.NewMsgID(spectypes.DomainType{}, make([]byte, 48), spectypes.RolePTCAttester)

	limit, ok := mv.dutyLimit(msgID, 0, nil)
	require.True(t, ok)
	require.Equal(t, mv.netCfg.SlotsPerEpoch, limit)
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
