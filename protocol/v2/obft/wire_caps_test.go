package obft_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/base"
	basewire "github.com/ssvlabs/ssv/protocol/v2/obft/base/wire"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	twoabwire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

// TestWireCaps_ReconciledAcrossProtocols asserts both OBFT-family wire codecs
// resolve their caps to the single shared obft definition — i.e. the historical
// base/twoab cap drift (twoab's coarse 16 MiB vs base's tight per-field caps) is
// gone and cannot silently reappear.
func TestWireCaps_ReconciledAcrossProtocols(t *testing.T) {
	require.Equal(t, obft.MaxLayers, basewire.MaxLayers)
	require.Equal(t, obft.MaxLayers, twoabwire.MaxLayers)
	require.Equal(t, obft.MaxValueSize, basewire.MaxValueSize)
	require.Equal(t, obft.MaxValueSize, twoabwire.MaxValueSize)
	require.Equal(t, obft.MaxSignatureSize, basewire.MaxSignatureSize)
	require.Equal(t, obft.MaxSignatureSize, twoabwire.MaxSignatureSize)
	require.Equal(t, obft.MaxCiphertextSize, basewire.MaxCiphertextSize)
	require.Equal(t, obft.MaxCiphertextSize, twoabwire.MaxCiphertextSize)
}

// TestWireLayerIndexBound_RejectedByBothProtocols pins the reconciled layer-
// index bound: a layer index equal to MaxLayers is out of range (valid indices
// are [0, MaxLayers)) and must be rejected by both decoders. This is the
// regression guard for the historical off-by-one — twoab previously used
// `layer > MaxLayers` and silently ACCEPTED index == MaxLayers, while base
// (correctly) used `layer >= MaxLayers`.
func TestWireLayerIndexBound_RejectedByBothProtocols(t *testing.T) {
	const outOfRange = obft.MaxLayers // == MaxLayers: first invalid index

	t.Run("base", func(t *testing.T) {
		b := &base.Phase1Bundle{
			OperatorID:  1,
			Height:      1,
			Layer:       outOfRange,
			Value:       []byte{0x01},
			LeaderSigma: []byte{0x01},
		}
		enc, err := basewire.EncodePhase1Bundle(b)
		require.NoError(t, err, "encoder has no upper layer bound; the decoder enforces it")
		_, err = basewire.DecodePhase1Bundle(enc)
		require.ErrorContains(t, err, "exceeds MaxLayers")
	})

	t.Run("twoab", func(t *testing.T) {
		b := &twoab.Phase1Bundle{
			OperatorID:  1,
			Height:      1,
			Layer:       outOfRange,
			Value:       []byte{0x01},
			LeaderSigma: []byte{0x01},
		}
		enc, err := twoabwire.EncodePhase1Bundle(b)
		require.NoError(t, err, "encoder has no upper layer bound; the decoder enforces it")
		_, err = twoabwire.DecodePhase1Bundle(enc)
		require.ErrorContains(t, err, "exceeds MaxLayers")
	})
}

// TestWireLayerIndexBound_AcceptsMaxIndex is the companion: the largest valid
// index (MaxLayers-1) must round-trip on both protocols, confirming the bound
// is exclusive (not off-by-one in the strict direction either).
func TestWireLayerIndexBound_AcceptsMaxIndex(t *testing.T) {
	const maxValid = obft.MaxLayers - 1

	t.Run("base", func(t *testing.T) {
		b := &base.Phase1Bundle{OperatorID: 1, Height: 1, Layer: maxValid, Value: []byte{0x01}, LeaderSigma: []byte{0x01}}
		enc, err := basewire.EncodePhase1Bundle(b)
		require.NoError(t, err)
		got, err := basewire.DecodePhase1Bundle(enc)
		require.NoError(t, err)
		require.Equal(t, maxValid, got.Layer)
	})

	t.Run("twoab", func(t *testing.T) {
		b := &twoab.Phase1Bundle{OperatorID: 1, Height: 1, Layer: maxValid, Value: []byte{0x01}, LeaderSigma: []byte{0x01}}
		enc, err := twoabwire.EncodePhase1Bundle(b)
		require.NoError(t, err)
		got, err := twoabwire.DecodePhase1Bundle(enc)
		require.NoError(t, err)
		require.Equal(t, maxValid, got.Layer)
	})
}
