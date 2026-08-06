package networkconfig

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// computeExpectedIsAggregatorSelected is a recompute of the aggregator-selection formula,
// mirroring the documented spec pseudocode. Being a mirror, it catches a divergence in one copy
// but not a conceptual error replicated in both (e.g. wrong endianness); the golden vectors in
// TestIsAggregatorSelected_GoldenVectors pin known-good outputs for that.
func computeExpectedIsAggregatorSelected(targetAggregatorsPerCommittee, committeeLength uint64, slotSig []byte) bool {
	modulo := uint64(1)
	if targetAggregatorsPerCommittee > 0 {
		modulo = max(1, committeeLength/targetAggregatorsPerCommittee)
	}
	h := sha256.Sum256(slotSig)
	x := binary.LittleEndian.Uint64(h[:8])
	return x%modulo == 0
}

func TestIsAggregatorSelected(t *testing.T) {
	t.Parallel()

	slotSigA := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	slotSigB := []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	testCases := []struct {
		name            string
		target          uint64
		committeeLength uint64
		slotSig         []byte
	}{
		{name: "small committee forces modulo to one", target: 16, committeeLength: 2, slotSig: slotSigA},
		{name: "zero committee length forces modulo to one", target: 16, committeeLength: 0, slotSig: slotSigA},
		{name: "committeeLength equal to target forces modulo to one", target: 16, committeeLength: 16, slotSig: slotSigA},
		{name: "large committee uses computed modulo (sig A)", target: 16, committeeLength: 128, slotSig: slotSigA},
		{name: "large committee uses computed modulo (sig B)", target: 16, committeeLength: 128, slotSig: slotSigB},
		{name: "small target", target: 3, committeeLength: 10, slotSig: slotSigA},
		{name: "zero target clamps to modulo one instead of panicking", target: 0, committeeLength: 128, slotSig: slotSigA},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := IsAggregatorSelected(tc.target, tc.committeeLength, tc.slotSig)
			want := computeExpectedIsAggregatorSelected(tc.target, tc.committeeLength, tc.slotSig)
			require.Equal(t, want, got)
		})
	}
}

// TestIsAggregatorSelected_GoldenVectors pins the selection formula to hardcoded known-good
// outputs, so a conceptual error present in both the implementation and the mirrored recompute
// above (e.g. wrong endianness, hash, or slice bounds) still fails.
//
// Golden values: sha256(64×'a')[0:8] little-endian = 7911663989264015615,
// sha256(64×'b')[0:8] little-endian = 6460213001230351008; selected ⇔ value % modulo == 0 with
// modulo = max(1, committeeLength/target).
func TestIsAggregatorSelected_GoldenVectors(t *testing.T) {
	t.Parallel()

	slotSigA := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	slotSigB := []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	testCases := []struct {
		name            string
		target          uint64
		committeeLength uint64
		slotSig         []byte
		want            bool
	}{
		{name: "sig A, modulo 8, not selected", target: 16, committeeLength: 128, slotSig: slotSigA, want: false},
		{name: "sig B, modulo 8, selected", target: 16, committeeLength: 128, slotSig: slotSigB, want: true},
		{name: "sig A, modulo clamped to 1, always selected", target: 16, committeeLength: 2, slotSig: slotSigA, want: true},
		{name: "sig A, modulo 100, not selected", target: 1, committeeLength: 100, slotSig: slotSigA, want: false},
		{name: "sig A, modulo 3, not selected", target: 3, committeeLength: 10, slotSig: slotSigA, want: false},
		{name: "sig B, modulo 3, selected", target: 3, committeeLength: 10, slotSig: slotSigB, want: true},
		{name: "zero target clamps to modulo 1, always selected", target: 0, committeeLength: 10, slotSig: slotSigA, want: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, IsAggregatorSelected(tc.target, tc.committeeLength, tc.slotSig))
		})
	}
}
