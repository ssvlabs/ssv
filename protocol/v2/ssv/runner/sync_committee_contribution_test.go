package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

// TestSortBySubnet pins the deterministic, cross-node canonical ordering that
// sortBySubnet provides for sync-committee contributions. Two nodes building
// contributions from the same logical set must agree on the resulting SSZ root,
// which requires an identical (subnet-ascending) order before
// GetSyncCommitteeContribution — otherwise the contribution roots diverge and the
// duty silently fails. This is a standalone unit test with no dependency on the
// runner test-kit (per PR #2899 review); the broader end-to-end regression test
// lands with the rest of upstream #2859.
func TestSortBySubnet(t *testing.T) {
	// mk builds a pair whose proof encodes its subnet in the first byte, so we can
	// assert the (subnet, proof) pairing stays intact across the sort.
	mk := func(subnet uint64) subnetSelectionProof {
		var p phase0.BLSSignature
		p[0] = byte(subnet)
		return subnetSelectionProof{subnet: subnet, selectionProof: p}
	}

	t.Run("orders ascending and preserves pairing", func(t *testing.T) {
		pairs := []subnetSelectionProof{mk(7), mk(0), mk(3), mk(12), mk(1)}

		sortBySubnet(pairs)

		got := make([]uint64, len(pairs))
		for i, p := range pairs {
			got[i] = p.subnet
			require.Equal(t, byte(p.subnet), p.selectionProof[0], "proof must stay paired with its subnet")
		}
		require.Equal(t, []uint64{0, 1, 3, 7, 12}, got)
	})

	t.Run("already sorted stays in order", func(t *testing.T) {
		pairs := []subnetSelectionProof{mk(0), mk(1), mk(2)}
		sortBySubnet(pairs)
		require.Equal(t, []uint64{0, 1, 2}, []uint64{pairs[0].subnet, pairs[1].subnet, pairs[2].subnet})
	})

	t.Run("empty and nil do not panic", func(t *testing.T) {
		require.NotPanics(t, func() { sortBySubnet(nil) })
		require.NotPanics(t, func() { sortBySubnet([]subnetSelectionProof{}) })
	})
}
