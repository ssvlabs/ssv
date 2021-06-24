package operator

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestGetSlotStartTime(t *testing.T) {
	n := operatorNode{logger: zap.L(), ethNetwork: core.NetworkFromString("prater")}

	ts := n.getSlotStartTime(646523)
	require.Equal(t, int64(1624266276), ts.Unix())
}

func TestGetCurrentSlot(t *testing.T) {
	n := operatorNode{logger: zap.L(), ethNetwork: core.NetworkFromString("prater")}

	slot := n.getCurrentSlot()
	require.Greater(t, slot, int64(646855))
}

func TestGetEpochFirstSlot(t *testing.T) {
	n := operatorNode{logger: zap.L(), ethNetwork: core.NetworkFromString("prater")}

	slot := n.getEpochFirstSlot(20203)
	require.Equal(t, uint64(646496), slot)
}
