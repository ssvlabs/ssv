package wire

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
)

func TestWrapUnwrap_Onion(t *testing.T) {
	original := &tbft.Onion{
		OperatorID: 5,
		Height:     42,
		Layers: []tbft.EncryptedLayer{
			{Tag: nil, Value: tbft.Value("v0"), Ciphertext: []byte("ct0")},
			{Tag: []byte("tag1"), Value: tbft.Value("v1"), Ciphertext: []byte("ct1")},
		},
	}

	wrapped, err := WrapOnion(original)
	require.NoError(t, err)

	// Sanity: envelope starts with version + kind.
	require.GreaterOrEqual(t, len(wrapped), 2)
	require.Equal(t, EnvelopeVersionV1, wrapped[0])
	require.Equal(t, byte(KindOnion), wrapped[1])

	env, err := Unwrap(wrapped)
	require.NoError(t, err)
	require.Equal(t, KindOnion, env.Kind)
	require.NotNil(t, env.Onion)
	require.Nil(t, env.NonReceipt)

	require.Equal(t, original.OperatorID, env.Onion.OperatorID)
	require.Equal(t, original.Height, env.Onion.Height)
	require.Len(t, env.Onion.Layers, 2)
	require.True(t, bytes.Equal(original.Layers[0].Value, env.Onion.Layers[0].Value))
	require.True(t, bytes.Equal(original.Layers[1].Tag, env.Onion.Layers[1].Tag))
}

func TestWrapUnwrap_NonReceipt(t *testing.T) {
	original := &tbft.NonReceiptAttestation{
		OperatorID: 3,
		Height:     99,
		Layer:      1,
		PartialSig: tbft.Signature("partial-sig-bytes"),
	}

	wrapped, err := WrapNonReceipt(original)
	require.NoError(t, err)
	require.Equal(t, EnvelopeVersionV1, wrapped[0])
	require.Equal(t, byte(KindNonReceipt), wrapped[1])

	env, err := Unwrap(wrapped)
	require.NoError(t, err)
	require.Equal(t, KindNonReceipt, env.Kind)
	require.Nil(t, env.Onion)
	require.NotNil(t, env.NonReceipt)

	require.Equal(t, original.OperatorID, env.NonReceipt.OperatorID)
	require.Equal(t, original.Height, env.NonReceipt.Height)
	require.Equal(t, original.Layer, env.NonReceipt.Layer)
	require.True(t, bytes.Equal(original.PartialSig, env.NonReceipt.PartialSig))
}

func TestUnwrap_Truncated(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"only version", []byte{EnvelopeVersionV1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Unwrap(tc.data)
			require.ErrorContains(t, err, "truncated")
		})
	}
}

func TestUnwrap_BadVersion(t *testing.T) {
	_, err := Unwrap([]byte{0xFF, byte(KindOnion)})
	require.ErrorContains(t, err, "unsupported envelope version")
}

func TestUnwrap_UnknownKind(t *testing.T) {
	_, err := Unwrap([]byte{EnvelopeVersionV1, 0xFE, 0x01})
	require.ErrorContains(t, err, "unknown envelope kind")
}

func TestUnwrap_OnionWithBadBody(t *testing.T) {
	// Envelope advertises KindOnion but body is garbage.
	bad := []byte{EnvelopeVersionV1, byte(KindOnion), 0x00, 0x01, 0x02}
	_, err := Unwrap(bad)
	require.ErrorContains(t, err, "decode onion body")
}

func TestUnwrap_NonReceiptWithBadBody(t *testing.T) {
	bad := []byte{EnvelopeVersionV1, byte(KindNonReceipt), 0x00}
	_, err := Unwrap(bad)
	require.ErrorContains(t, err, "decode non-receipt body")
}

func TestWrapOnion_NilRejected(t *testing.T) {
	_, err := WrapOnion(nil)
	require.Error(t, err)
}

func TestWrapNonReceipt_NilRejected(t *testing.T) {
	_, err := WrapNonReceipt(nil)
	require.Error(t, err)
}

func TestWrapUnwrap_Candidate(t *testing.T) {
	original := &tbft.CandidateBroadcast{
		OperatorID: 9,
		Height:     321,
		Layer:      0,
		Value:      tbft.Value("phase-1-broadcast-block"),
	}
	wrapped, err := WrapCandidate(original)
	require.NoError(t, err)
	require.Equal(t, EnvelopeVersionV1, wrapped[0])
	require.Equal(t, byte(KindCandidate), wrapped[1])

	env, err := Unwrap(wrapped)
	require.NoError(t, err)
	require.Equal(t, KindCandidate, env.Kind)
	require.Nil(t, env.Onion)
	require.Nil(t, env.NonReceipt)
	require.NotNil(t, env.Candidate)

	require.Equal(t, original.OperatorID, env.Candidate.OperatorID)
	require.Equal(t, original.Height, env.Candidate.Height)
	require.Equal(t, original.Layer, env.Candidate.Layer)
	require.True(t, bytes.Equal(original.Value, env.Candidate.Value))
}

func TestUnwrap_CandidateWithBadBody(t *testing.T) {
	bad := []byte{EnvelopeVersionV1, byte(KindCandidate), 0x00}
	_, err := Unwrap(bad)
	require.ErrorContains(t, err, "decode candidate body")
}

func TestWrapCandidate_NilRejected(t *testing.T) {
	_, err := WrapCandidate(nil)
	require.Error(t, err)
}
