package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

// TestSortBySubnet pins the canonicalization step that the runner applies to the
// collected (subnet, selection-proof) pairs before calling GetSyncCommitteeContribution.
// See sortBySubnet for the full rationale.
//
// Covers the two properties the fix needs to guarantee — determinism across runs and
// spec-canonical ascending-by-subnet ordering — by testing the helper directly. A full
// runner-level test was considered but the helper is the only behavior the fix introduces;
// everything upstream is unchanged.
func TestSortBySubnet(t *testing.T) {
	// sig builds a phase0.BLSSignature with byte[0] set to tag for identity tracking.
	sig := func(tag byte) phase0.BLSSignature {
		var s phase0.BLSSignature
		s[0] = tag
		return s
	}

	t.Run("sorts ascending and preserves (subnet, proof) pairing", func(t *testing.T) {
		pairs := []subnetSelectionProof{
			{subnet: 3, selectionProof: sig(0xa)},
			{subnet: 1, selectionProof: sig(0xb)},
			{subnet: 2, selectionProof: sig(0xc)},
		}
		sortBySubnet(pairs)
		require.Equal(t, []subnetSelectionProof{
			{subnet: 1, selectionProof: sig(0xb)},
			{subnet: 2, selectionProof: sig(0xc)},
			{subnet: 3, selectionProof: sig(0xa)},
		}, pairs)
	})

	t.Run("deterministic across repeated runs with the same input", func(t *testing.T) {
		const iterations = 100
		var first []subnetSelectionProof
		for i := 0; i < iterations; i++ {
			pairs := []subnetSelectionProof{
				{subnet: 3, selectionProof: sig(0xa)},
				{subnet: 1, selectionProof: sig(0xb)},
				{subnet: 2, selectionProof: sig(0xc)},
				{subnet: 0, selectionProof: sig(0xd)},
			}
			sortBySubnet(pairs)
			if i == 0 {
				first = append([]subnetSelectionProof(nil), pairs...)
				continue
			}
			require.Equalf(t, first, pairs, "iteration %d: result differs", i)
		}
	})

	t.Run("matches spec canonical ordering (ascending SubcommitteeIndex)", func(t *testing.T) {
		// ssv-spec test fixture appends contributions with SubcommitteeIndex [0, 1, 2]
		// (see ssv-spec/types/testingutils/beacon_node_sync_committee.go). Verify our
		// sort produces that exact order regardless of input permutation.
		pairs := []subnetSelectionProof{
			{subnet: 2, selectionProof: sig(2)},
			{subnet: 0, selectionProof: sig(0)},
			{subnet: 1, selectionProof: sig(1)},
		}
		sortBySubnet(pairs)
		require.Equal(t, []subnetSelectionProof{
			{subnet: 0, selectionProof: sig(0)},
			{subnet: 1, selectionProof: sig(1)},
			{subnet: 2, selectionProof: sig(2)},
		}, pairs)
	})

	t.Run("empty slice is a no-op", func(t *testing.T) {
		pairs := []subnetSelectionProof{}
		sortBySubnet(pairs)
		require.Empty(t, pairs)
	})

	t.Run("single-element slice is unchanged", func(t *testing.T) {
		pairs := []subnetSelectionProof{{subnet: 5, selectionProof: sig(0x42)}}
		sortBySubnet(pairs)
		require.Equal(t, []subnetSelectionProof{{subnet: 5, selectionProof: sig(0x42)}}, pairs)
	})

	t.Run("already-sorted input is unchanged", func(t *testing.T) {
		pairs := []subnetSelectionProof{
			{subnet: 0, selectionProof: sig(0x10)},
			{subnet: 1, selectionProof: sig(0x11)},
			{subnet: 2, selectionProof: sig(0x12)},
			{subnet: 3, selectionProof: sig(0x13)},
		}
		sortBySubnet(pairs)
		require.Equal(t, []subnetSelectionProof{
			{subnet: 0, selectionProof: sig(0x10)},
			{subnet: 1, selectionProof: sig(0x11)},
			{subnet: 2, selectionProof: sig(0x12)},
			{subnet: 3, selectionProof: sig(0x13)},
		}, pairs)
	})
}
