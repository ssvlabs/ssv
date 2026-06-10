package runner

import (
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/prysmaticlabs/go-bitfield"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestContributionsSSZEncodingMatchesSpec pins ssv-spec's current SSZ encoding
// for spectypes.Contributions: variable-length-list framing (per-element 4-byte
// offsets) applied to a list whose element type Contribution is fixed-length
// 256 bytes. The hand-written encoder is at ssv-spec/types/consensus_data.go:73-106
// (https://github.com/ssvlabs/ssv-spec/blob/v1.2.2/types/consensus_data.go#L73-L106);
// the fastssz-generated Contribution.SizeSSZ at consensus_data_encoding.go:52
// confirms the element is fixed-length 256 bytes.
//
// Both SSV-Go (via direct spec import) and Anchor (via ContributionWrapper)
// implement this encoding faithfully, so the system works and there is no
// cross-client divergence to resolve. This test is a regression guard: if
// ssv-spec ever changes to canonical-SSZ framing (plain concatenation, no
// per-element offsets), this test will fail loudly here in SSV-Go before any
// wire-format change reaches production.
//
// Expected encoding for a list of N=2 Contributions:
//   - 2 × 4-byte offsets (8 bytes total): offsets[0] = 8, offsets[1] = 264.
//   - 2 × 256-byte fixed-length Contribution bodies.
//   - Total: 8 + 512 = 520 bytes.
//
// Canonical-SSZ alternative (NOT what the spec currently does) would be
// 2 × 256 = 512 bytes (plain concatenation, no offsets).
func TestContributionsSSZEncodingMatchesSpec(t *testing.T) {
	makeContribution := func(seed byte) *spectypes.Contribution {
		var sig phase0.BLSSignature
		for i := range sig {
			sig[i] = seed
		}
		var root phase0.Root
		for i := range root {
			root[i] = seed ^ 0x55
		}
		aggBits := bitfield.Bitvector128(make([]byte, 16))
		for i := range aggBits {
			aggBits[i] = seed
		}
		return &spectypes.Contribution{
			SelectionProofSig: sig,
			Contribution: altair.SyncCommitteeContribution{
				Slot:              phase0.Slot(1 + uint64(seed)),
				BeaconBlockRoot:   root,
				SubcommitteeIndex: uint64(seed),
				AggregationBits:   aggBits,
				Signature:         sig,
			},
		}
	}

	c0 := makeContribution(0x11)
	c1 := makeContribution(0x22)
	contribs := spectypes.Contributions{c0, c1}

	encoded, err := contribs.MarshalSSZ()
	require.NoError(t, err)

	// Single-Contribution size must remain 256 bytes (fixed-length per fastssz
	// codegen at ssv-spec/types/consensus_data_encoding.go:52).
	const expectedSingleSize = 256
	require.Equal(t, expectedSingleSize, c0.SizeSSZ(),
		"Contribution.SizeSSZ() must remain 256; if this fails, the underlying spec types changed shape")

	// The spec encodes Contributions with variable-length-list framing:
	// 2 × 4-byte offsets + 2 × 256-byte elements = 520 bytes.
	const expectedSpecLen = 2*4 + 2*expectedSingleSize
	require.Equalf(t, expectedSpecLen, len(encoded),
		"unexpected Contributions encoding size %d (want %d). "+
			"If the spec switched to canonical-SSZ framing (plain concatenation), "+
			"this needs a coordinated cross-client rollout with Anchor (drop ContributionWrapper).",
		len(encoded), expectedSpecLen)

	// First 8 bytes must be two little-endian 4-byte offsets pointing at the
	// element bodies: offsets[0] = 8 (just past the offset table),
	// offsets[1] = 8 + 256 = 264.
	require.GreaterOrEqual(t, len(encoded), 8)
	off0 := uint32(encoded[0]) | uint32(encoded[1])<<8 | uint32(encoded[2])<<16 | uint32(encoded[3])<<24
	off1 := uint32(encoded[4]) | uint32(encoded[5])<<8 | uint32(encoded[6])<<16 | uint32(encoded[7])<<24
	require.Equalf(t, uint32(8), off0, "offset[0] must be 8 (start of first element), got %d", off0)
	require.Equalf(t, uint32(8+expectedSingleSize), off1,
		"offset[1] must be %d (start of second element), got %d", 8+expectedSingleSize, off1)

	t.Logf("OK: Contributions{c0, c1} encoded as %d bytes via variable-length-list framing", len(encoded))
	t.Logf("    offsets = [%d, %d], first 32 bytes = %s",
		off0, off1, hex.EncodeToString(encoded[:32]))
}
