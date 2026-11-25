package p2pv1

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMeshDials checks meshDials' plan for every net size up to 33 nodes: each pair of nodes is dialed exactly once,
// no node dials itself, and inbound dials spread evenly, at most n/2 per node and at most one apart across nodes.
func TestMeshDials(t *testing.T) {
	for n := range 34 {
		dials := meshDials(n)

		edges := make(map[meshDial]bool, len(dials))
		inbound := make([]int, n)
		for _, d := range dials {
			require.True(t, d.from >= 0 && d.from < n && d.to >= 0 && d.to < n, "n=%d: dial %v is out of range", n, d)
			require.NotEqual(t, d.from, d.to, "n=%d: node %d dials itself", n, d.from)
			edge := meshDial{from: min(d.from, d.to), to: max(d.from, d.to)}
			require.False(t, edges[edge], "n=%d: nodes %d and %d are dialed twice", n, edge.from, edge.to)
			edges[edge] = true
			inbound[d.to]++
		}
		require.Len(t, edges, n*(n-1)/2, "n=%d: some pair of nodes is never dialed", n)

		if n > 0 {
			most, least := slices.Max(inbound), slices.Min(inbound)
			require.LessOrEqual(t, most, n/2, "n=%d: a node takes %d inbound dials", n, most)
			require.LessOrEqual(t, most-least, 1, "n=%d: inbound dials range from %d to %d per node", n, least, most)
		}
	}
}
