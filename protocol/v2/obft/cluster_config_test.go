package obft_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// topologyForK builds a minimal, otherwise-valid (operators, layers) pair with
// K layers over K operators: unique leaders, equal budgets, fetchAt=0. It lets
// the layer-count bounds in ValidateClusterTopology be exercised in isolation
// (the leader/budget/fetchAt checks come after the count checks).
func topologyForK(k int) ([]obft.OperatorID, []obft.LayerSpec) {
	operators := make([]obft.OperatorID, k)
	layers := make([]obft.LayerSpec, k)
	for i := 0; i < k; i++ {
		operators[i] = obft.OperatorID(i + 1)
		layers[i] = obft.LayerSpec{
			Leader:          obft.OperatorID(i + 1),
			FetchAt:         0,
			BroadcastBudget: time.Second,
		}
	}
	return operators, layers
}

// TestValidateClusterTopology_AcceptsKAtMaxLayers pins the upper boundary: a
// layer count exactly equal to MaxLayers is the largest K the wire codec can
// represent (valid layer indices are [0, MaxLayers)), so config validation
// must accept it.
func TestValidateClusterTopology_AcceptsKAtMaxLayers(t *testing.T) {
	operators, layers := topologyForK(obft.MaxLayers)
	require.NoError(t, obft.ValidateClusterTopology(operators, 1, layers, 200*time.Millisecond))
}

// TestValidateClusterTopology_RejectsKAboveMaxLayers guards the alignment
// between config validation and the wire cap: a K past MaxLayers would declare
// a layer index the decoder rejects (>= MaxLayers), so it must be caught at
// config time rather than surfacing later as an undecodable message. The
// check is independent of the cluster-size bound — here len(operators) ==
// len(layers), so the MaxLayers branch (not "K cannot exceed cluster size") is
// what fires.
func TestValidateClusterTopology_RejectsKAboveMaxLayers(t *testing.T) {
	operators, layers := topologyForK(obft.MaxLayers + 1)
	err := obft.ValidateClusterTopology(operators, 1, layers, 200*time.Millisecond)
	require.ErrorContains(t, err, "exceeds wire cap MaxLayers")
}
