package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

// TestSortContributionsBySubnet pins the canonicalisation step that the runner applies
// to the paired (subnets, selectionProofs) slices before calling GetSyncCommitteeContribution.
// See sortContributionsBySubnet for the full rationale.
//
// This indirectly covers the two test ideas proposed in
// docs/v2.4.3-QA-verification-plan.md §B2 §6 — determinism across runs and spec-canonical
// ordering — by testing the small helper directly. A full runner-level test was considered
// but the helper is the only behaviour the fix introduces; everything upstream is unchanged.
func TestSortContributionsBySubnet(t *testing.T) {
	// sig builds a phase0.BLSSignature with byte[0] set to tag for identity tracking.
	sig := func(tag byte) phase0.BLSSignature {
		var s phase0.BLSSignature
		s[0] = tag
		return s
	}

	t.Run("sorts ascending and preserves (subnet, proof) pairing", func(t *testing.T) {
		subnets := []uint64{3, 1, 2}
		proofs := []phase0.BLSSignature{sig(0xa), sig(0xb), sig(0xc)}
		sortContributionsBySubnet(subnets, proofs)
		require.Equal(t, []uint64{1, 2, 3}, subnets)
		// 1 came from idx 1 → sig(0xb); 2 from idx 2 → sig(0xc); 3 from idx 0 → sig(0xa).
		require.Equal(t, []phase0.BLSSignature{sig(0xb), sig(0xc), sig(0xa)}, proofs)
	})

	t.Run("deterministic across repeated runs with the same input", func(t *testing.T) {
		const iterations = 100
		var firstSubnets []uint64
		var firstProofs []phase0.BLSSignature
		for i := 0; i < iterations; i++ {
			subnets := []uint64{3, 1, 2, 0}
			proofs := []phase0.BLSSignature{sig(0xa), sig(0xb), sig(0xc), sig(0xd)}
			sortContributionsBySubnet(subnets, proofs)
			if i == 0 {
				firstSubnets = append([]uint64(nil), subnets...)
				firstProofs = append([]phase0.BLSSignature(nil), proofs...)
				continue
			}
			require.Equalf(t, firstSubnets, subnets, "iteration %d: subnets differ", i)
			require.Equalf(t, firstProofs, proofs, "iteration %d: proofs differ", i)
		}
	})

	t.Run("matches spec canonical ordering (ascending SubcommitteeIndex)", func(t *testing.T) {
		// ssv-spec test fixture appends contributions with SubcommitteeIndex [0, 1, 2]
		// (see ssv-spec/types/testingutils/beacon_node_sync_committee.go). Verify our
		// sort produces that exact order regardless of input permutation.
		subnets := []uint64{2, 0, 1}
		proofs := []phase0.BLSSignature{sig(2), sig(0), sig(1)}
		sortContributionsBySubnet(subnets, proofs)
		require.Equal(t, []uint64{0, 1, 2}, subnets)
		require.Equal(t, sig(0), proofs[0])
		require.Equal(t, sig(1), proofs[1])
		require.Equal(t, sig(2), proofs[2])
	})

	t.Run("empty slices are a no-op", func(t *testing.T) {
		subnets := []uint64{}
		proofs := []phase0.BLSSignature{}
		sortContributionsBySubnet(subnets, proofs)
		require.Empty(t, subnets)
		require.Empty(t, proofs)
	})

	t.Run("single-element slice is unchanged", func(t *testing.T) {
		subnets := []uint64{5}
		proofs := []phase0.BLSSignature{sig(0x42)}
		sortContributionsBySubnet(subnets, proofs)
		require.Equal(t, []uint64{5}, subnets)
		require.Equal(t, []phase0.BLSSignature{sig(0x42)}, proofs)
	})

	t.Run("already-sorted input is unchanged", func(t *testing.T) {
		subnets := []uint64{0, 1, 2, 3}
		proofs := []phase0.BLSSignature{sig(0x10), sig(0x11), sig(0x12), sig(0x13)}
		sortContributionsBySubnet(subnets, proofs)
		require.Equal(t, []uint64{0, 1, 2, 3}, subnets)
		require.Equal(t, []phase0.BLSSignature{sig(0x10), sig(0x11), sig(0x12), sig(0x13)}, proofs)
	})

	t.Run("mismatched lengths panic", func(t *testing.T) {
		subnets := []uint64{1, 2}
		proofs := []phase0.BLSSignature{sig(0x1)}
		require.Panics(t, func() {
			sortContributionsBySubnet(subnets, proofs)
		})
	})
}
