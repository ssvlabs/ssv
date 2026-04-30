package wire

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
)

// ---- Onion round-trip ----------------------------------------------------

func TestEncodeDecodeOnion_RoundTrip(t *testing.T) {
	original := &tbft.Onion{
		OperatorID: 42,
		Height:     1234567,
		Layers: []tbft.EncryptedLayer{
			{
				Tag:        nil, // layer 0: plaintext
				Value:      tbft.Value("layer-0-value"),
				Ciphertext: []byte("layer-0-partial-sig-bytes"),
			},
			{
				Tag:        []byte("no-quorum-tag-for-layer-0"),
				Value:      tbft.Value("layer-1-value"),
				Ciphertext: []byte("layer-1-encrypted-partial-sig-bytes"),
			},
			{
				// Empty layer — operator did not contribute at this index.
				Tag:        nil,
				Value:      nil,
				Ciphertext: nil,
			},
		},
	}

	encoded, err := EncodeOnion(original)
	require.NoError(t, err)

	decoded, err := DecodeOnion(encoded)
	require.NoError(t, err)

	require.Equal(t, original.OperatorID, decoded.OperatorID)
	require.Equal(t, original.Height, decoded.Height)
	require.Len(t, decoded.Layers, len(original.Layers))
	for i := range original.Layers {
		require.True(t, bytes.Equal(original.Layers[i].Tag, decoded.Layers[i].Tag),
			"layer %d tag mismatch", i)
		require.True(t, bytes.Equal(original.Layers[i].Value, decoded.Layers[i].Value),
			"layer %d value mismatch", i)
		require.True(t, bytes.Equal(original.Layers[i].Ciphertext, decoded.Layers[i].Ciphertext),
			"layer %d ciphertext mismatch", i)
	}
}

func TestEncodeOnion_NilOnion(t *testing.T) {
	_, err := EncodeOnion(nil)
	require.ErrorContains(t, err, "nil onion")
}

func TestEncodeOnion_TooManyLayers(t *testing.T) {
	o := &tbft.Onion{
		Layers: make([]tbft.EncryptedLayer, MaxLayers+1),
	}
	_, err := EncodeOnion(o)
	require.ErrorContains(t, err, "max")
}

func TestDecodeOnion_TruncatedAtEachField(t *testing.T) {
	original := &tbft.Onion{
		OperatorID: 1,
		Height:     100,
		Layers: []tbft.EncryptedLayer{
			{Tag: []byte("t"), Value: tbft.Value("v"), Ciphertext: []byte("c")},
		},
	}
	encoded, err := EncodeOnion(original)
	require.NoError(t, err)

	// Each truncation should fail decoding cleanly (not panic).
	for i := 0; i < len(encoded); i++ {
		_, err := DecodeOnion(encoded[:i])
		require.Error(t, err, "truncation at %d should fail", i)
		require.True(t, strings.Contains(err.Error(), "truncat") ||
			strings.Contains(err.Error(), "trailing") ||
			strings.Contains(err.Error(), "version"),
			"unexpected error at truncation %d: %v", i, err)
	}
}

func TestDecodeOnion_BadVersion(t *testing.T) {
	encoded := []byte{0xFF} // unknown version
	_, err := DecodeOnion(encoded)
	require.ErrorContains(t, err, "unsupported onion version")
}

func TestDecodeOnion_DeclaresTooManyLayers(t *testing.T) {
	// Construct a header that claims more layers than MaxLayers.
	out := []byte{OnionVersionV1}
	out = appendUint64(out, 1)           // operatorID
	out = appendUint64(out, 100)         // height
	out = appendUint16(out, MaxLayers+1) // too many
	_, err := DecodeOnion(out)
	require.ErrorContains(t, err, "max")
}

func TestDecodeOnion_TrailingBytes(t *testing.T) {
	o := &tbft.Onion{OperatorID: 1, Height: 1, Layers: []tbft.EncryptedLayer{}}
	encoded, _ := EncodeOnion(o)
	encoded = append(encoded, 0xAA) // garbage trailing byte
	_, err := DecodeOnion(encoded)
	require.ErrorContains(t, err, "trailing")
}

func TestDecodeOnion_ReturnsDefensiveCopy(t *testing.T) {
	// Decoded onion's byte slices must not alias the input buffer.
	original := &tbft.Onion{
		OperatorID: 1,
		Height:     1,
		Layers: []tbft.EncryptedLayer{
			{Tag: []byte("tag"), Value: tbft.Value("val"), Ciphertext: []byte("ct")},
		},
	}
	encoded, _ := EncodeOnion(original)
	decoded, _ := DecodeOnion(encoded)

	// Mutate the encoded buffer; the decoded onion should be unaffected.
	for i := range encoded {
		encoded[i] = 0x00
	}
	require.True(t, bytes.Equal(decoded.Layers[0].Value, []byte("val")),
		"decoded onion is aliased to encoded buffer")
}

// ---- NonReceipt round-trip ----------------------------------------------

func TestEncodeDecodeNonReceipt_RoundTrip(t *testing.T) {
	original := &tbft.NonReceiptAttestation{
		OperatorID: 7,
		Height:     999,
		Layer:      2,
		PartialSig: tbft.Signature("partial-bls-sig-bytes"),
	}
	encoded, err := EncodeNonReceipt(original)
	require.NoError(t, err)

	decoded, err := DecodeNonReceipt(encoded)
	require.NoError(t, err)

	require.Equal(t, original.OperatorID, decoded.OperatorID)
	require.Equal(t, original.Height, decoded.Height)
	require.Equal(t, original.Layer, decoded.Layer)
	require.True(t, bytes.Equal(original.PartialSig, decoded.PartialSig))
}

func TestEncodeNonReceipt_NilOrInvalid(t *testing.T) {
	_, err := EncodeNonReceipt(nil)
	require.ErrorContains(t, err, "nil non-receipt")

	_, err = EncodeNonReceipt(&tbft.NonReceiptAttestation{Layer: -1})
	require.ErrorContains(t, err, "negative layer")
}

func TestDecodeNonReceipt_BadVersion(t *testing.T) {
	encoded := []byte{0xFF}
	_, err := DecodeNonReceipt(encoded)
	require.ErrorContains(t, err, "unsupported non-receipt version")
}

func TestDecodeNonReceipt_TrailingBytes(t *testing.T) {
	nr := &tbft.NonReceiptAttestation{OperatorID: 1, Height: 1, Layer: 0, PartialSig: []byte{1}}
	encoded, _ := EncodeNonReceipt(nr)
	encoded = append(encoded, 0xAA)
	_, err := DecodeNonReceipt(encoded)
	require.ErrorContains(t, err, "trailing")
}

// ---- CandidateBroadcast round-trip --------------------------------------

func TestEncodeDecodeCandidate_RoundTrip(t *testing.T) {
	original := &tbft.CandidateBroadcast{
		OperatorID: 5,
		Height:     1234,
		Layer:      2,
		Value:      tbft.Value("the-blinded-block-bytes-go-here"),
	}
	encoded, err := EncodeCandidate(original)
	require.NoError(t, err)

	decoded, err := DecodeCandidate(encoded)
	require.NoError(t, err)

	require.Equal(t, original.OperatorID, decoded.OperatorID)
	require.Equal(t, original.Height, decoded.Height)
	require.Equal(t, original.Layer, decoded.Layer)
	require.True(t, bytes.Equal(original.Value, decoded.Value))
}

func TestEncodeCandidate_NilOrInvalid(t *testing.T) {
	_, err := EncodeCandidate(nil)
	require.ErrorContains(t, err, "nil candidate")

	_, err = EncodeCandidate(&tbft.CandidateBroadcast{Layer: -1})
	require.ErrorContains(t, err, "negative layer")
}

func TestDecodeCandidate_BadVersion(t *testing.T) {
	_, err := DecodeCandidate([]byte{0xFF})
	require.ErrorContains(t, err, "unsupported candidate version")
}

func TestDecodeCandidate_TrailingBytes(t *testing.T) {
	cb := &tbft.CandidateBroadcast{OperatorID: 1, Height: 1, Layer: 0, Value: []byte{0x01}}
	encoded, _ := EncodeCandidate(cb)
	encoded = append(encoded, 0xAA)
	_, err := DecodeCandidate(encoded)
	require.ErrorContains(t, err, "trailing")
}
