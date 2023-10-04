package beacon

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestNetwork_GetSlotEndTime(t *testing.T) {
	slot := phase0.Slot(1)

	n := NewNetwork(spectypes.PraterNetwork)
	slotStart := n.GetSlotStartTime(slot)
	slotEnd := n.GetSlotEndTime(slot)

	require.Equal(t, n.SlotDurationSec(), slotEnd.Sub(slotStart))
}
