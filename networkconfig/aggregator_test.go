package networkconfig

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// computeExpectedIsAggregatorSelected is an independent recompute of the aggregator-selection
// formula, mirroring the documented spec pseudocode so a bug in IsAggregatorSelected (e.g. wrong
// endianness or hash) fails this test rather than silently drifting.
func computeExpectedIsAggregatorSelected(targetAggregatorsPerCommittee, committeeCount uint64, slotSig []byte) bool {
	modulo := committeeCount / targetAggregatorsPerCommittee
	if modulo == 0 {
		modulo = 1
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
		name           string
		target         uint64
		committeeCount uint64
		slotSig        []byte
	}{
		{name: "small committee forces modulo to one", target: 16, committeeCount: 2, slotSig: slotSigA},
		{name: "zero committee count forces modulo to one", target: 16, committeeCount: 0, slotSig: slotSigA},
		{name: "committeeCount equal to target forces modulo to one", target: 16, committeeCount: 16, slotSig: slotSigA},
		{name: "large committee uses computed modulo (sig A)", target: 16, committeeCount: 128, slotSig: slotSigA},
		{name: "large committee uses computed modulo (sig B)", target: 16, committeeCount: 128, slotSig: slotSigB},
		{name: "small target", target: 3, committeeCount: 10, slotSig: slotSigA},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := IsAggregatorSelected(tc.target, tc.committeeCount, tc.slotSig)
			want := computeExpectedIsAggregatorSelected(tc.target, tc.committeeCount, tc.slotSig)
			require.Equal(t, want, got)
		})
	}
}
