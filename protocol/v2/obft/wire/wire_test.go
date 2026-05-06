package wire

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

func TestPhase1Bundle_Roundtrip(t *testing.T) {
	in := &obft.Phase1Bundle{
		OperatorID: 7,
		Height:     12345,
		Layer:      2,
		Value:      []byte("hello world"),
		SigmaV:     []byte{0x01, 0x02, 0x03, 0x04},
	}
	bytes_, err := WrapPhase1Bundle(in)
	require.NoError(t, err)

	env, err := Unwrap(bytes_)
	require.NoError(t, err)
	require.Equal(t, KindPhase1Bundle, env.Kind)
	require.Equal(t, in.OperatorID, env.Phase1Bundle.OperatorID)
	require.Equal(t, in.Height, env.Phase1Bundle.Height)
	require.Equal(t, in.Layer, env.Phase1Bundle.Layer)
	require.True(t, bytes.Equal(in.Value, env.Phase1Bundle.Value))
	require.True(t, bytes.Equal(in.SigmaV, env.Phase1Bundle.SigmaV))
}

func TestOnion_Roundtrip(t *testing.T) {
	in := &obft.Onion{
		OperatorID: 3,
		Height:     999,
		Layers: []obft.EncryptedLayer{
			{Value: []byte("V0"), Ciphertext: []byte("ct0")},
			{}, // empty (no contribution)
			{Value: []byte("V2"), Ciphertext: []byte("ciphertext-for-layer-2")},
			{Value: []byte("V3-deepest"), Ciphertext: []byte("CT")},
		},
	}
	bytes_, err := WrapOnion(in)
	require.NoError(t, err)

	env, err := Unwrap(bytes_)
	require.NoError(t, err)
	require.Equal(t, KindOnion, env.Kind)
	require.Equal(t, in.OperatorID, env.Onion.OperatorID)
	require.Equal(t, in.Height, env.Onion.Height)
	require.Len(t, env.Onion.Layers, len(in.Layers))
	for i, expected := range in.Layers {
		got := env.Onion.Layers[i]
		require.True(t, bytes.Equal(expected.Value, got.Value), "layer %d Value mismatch", i)
		require.True(t, bytes.Equal(expected.Ciphertext, got.Ciphertext), "layer %d Ciphertext mismatch", i)
	}
}

func TestNR_Roundtrip(t *testing.T) {
	in := &obft.NR{
		OperatorID: 5,
		Height:     42,
		Partials: []obft.NRPartial{
			{Layer: 0, PartialSig: []byte("sig0")},
			{Layer: 1, PartialSig: []byte("sig1-longer-than-the-first")},
		},
	}
	bytes_, err := WrapNR(in)
	require.NoError(t, err)

	env, err := Unwrap(bytes_)
	require.NoError(t, err)
	require.Equal(t, KindNR, env.Kind)
	require.Equal(t, in.OperatorID, env.NR.OperatorID)
	require.Equal(t, in.Height, env.NR.Height)
	require.Len(t, env.NR.Partials, len(in.Partials))
	for i, expected := range in.Partials {
		got := env.NR.Partials[i]
		require.Equal(t, expected.Layer, got.Layer)
		require.True(t, bytes.Equal(expected.PartialSig, got.PartialSig))
	}
}

func TestCertificate_Roundtrip(t *testing.T) {
	in := &obft.Certificate{
		Height:    77,
		Value:     []byte("decided value bytes"),
		Signature: []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
	}
	bytes_, err := WrapCertificate(in)
	require.NoError(t, err)

	env, err := Unwrap(bytes_)
	require.NoError(t, err)
	require.Equal(t, KindCertificate, env.Kind)
	require.Equal(t, in.Height, env.Certificate.Height)
	require.True(t, bytes.Equal(in.Value, env.Certificate.Value))
	require.True(t, bytes.Equal(in.Signature, env.Certificate.Signature))
}

func TestUnwrap_RejectsUnknownKind(t *testing.T) {
	// Forge an envelope with an unknown kind byte.
	data := []byte{EnvelopeVersionV1, 0x99, 0x00, 0x01, 0x02}
	_, err := Unwrap(data)
	require.ErrorContains(t, err, "unknown envelope kind")
}

func TestUnwrap_RejectsTruncated(t *testing.T) {
	_, err := Unwrap([]byte{EnvelopeVersionV1})
	require.Error(t, err)
}

func TestDecodePhase1Bundle_RejectsUnknownVersion(t *testing.T) {
	bogus := []byte{0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := DecodePhase1Bundle(bogus)
	require.ErrorContains(t, err, "unsupported phase-1 bundle version")
}

func TestEncodeOnion_RejectsTooManyLayers(t *testing.T) {
	o := &obft.Onion{Layers: make([]obft.EncryptedLayer, MaxLayers+1)}
	_, err := EncodeOnion(o)
	require.ErrorContains(t, err, "max")
}
