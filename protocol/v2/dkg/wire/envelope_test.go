package wire

import (
	"testing"

	"github.com/drand/kyber"
	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/share/dkg"
	"github.com/stretchr/testify/require"
)

func testSuite(t *testing.T) KyberSuite {
	t.Helper()
	return bls12381.NewBLS12381Suite().G1().(dkg.Suite)
}

func sampleClusterID() [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = byte(i + 1)
	}
	return out
}

func TestEnvelope_Exchange_RoundTrip(t *testing.T) {
	suite := testSuite(t)

	// Generate a fresh kyber G1 point to use as the operator's pubkey.
	scalar := suite.Scalar().Pick(suite.RandomStream())
	pt := suite.Point().Mul(scalar, nil)
	pkBytes, err := pt.MarshalBinary()
	require.NoError(t, err)

	in := &Exchange{
		ClusterID:  sampleClusterID(),
		Generation: 7,
		OperatorID: 42,
		PubKey:     pkBytes,
	}

	wrapped, err := WrapExchange(in)
	require.NoError(t, err)
	require.Equal(t, byte(EnvelopeVersionV1), wrapped[0])
	require.Equal(t, byte(KindExchange), wrapped[1])

	env, err := Unwrap(wrapped, suite)
	require.NoError(t, err)
	require.Equal(t, KindExchange, env.Kind)
	require.NotNil(t, env.Exchange)
	require.Nil(t, env.Deal)
	require.Equal(t, in.ClusterID, env.Exchange.ClusterID)
	require.Equal(t, in.Generation, env.Exchange.Generation)
	require.Equal(t, in.OperatorID, env.Exchange.OperatorID)
	require.Equal(t, in.PubKey, env.Exchange.PubKey)
}

func TestEnvelope_Deal_RoundTrip(t *testing.T) {
	suite := testSuite(t)

	// Build a deal bundle whose Public[] holds two G1 points so the
	// hex-encoding round-trip is exercised in earnest.
	pub0 := suite.Point().Mul(suite.Scalar().Pick(suite.RandomStream()), nil)
	pub1 := suite.Point().Mul(suite.Scalar().Pick(suite.RandomStream()), nil)
	bundle := &dkg.DealBundle{
		DealerIndex: 1,
		Deals: []dkg.Deal{
			{ShareIndex: 2, EncryptedShare: []byte{0xaa, 0xbb}},
			{ShareIndex: 3, EncryptedShare: []byte{0xcc, 0xdd}},
		},
		Public:    []kyber.Point{pub0, pub1},
		SessionID: []byte("session-deal"),
		Signature: []byte("sig-deal"),
	}

	in := &DealEnvelope{
		ClusterID:  sampleClusterID(),
		Generation: 7,
		Bundle:     bundle,
	}

	wrapped, err := WrapDeal(in)
	require.NoError(t, err)
	require.Equal(t, byte(KindDeal), wrapped[1])

	env, err := Unwrap(wrapped, suite)
	require.NoError(t, err)
	require.Equal(t, KindDeal, env.Kind)
	require.NotNil(t, env.Deal)
	require.Equal(t, in.ClusterID, env.Deal.ClusterID)
	require.Equal(t, in.Generation, env.Deal.Generation)
	require.Equal(t, bundle.DealerIndex, env.Deal.Bundle.DealerIndex)
	require.Equal(t, bundle.Deals, env.Deal.Bundle.Deals)
	require.Equal(t, bundle.SessionID, env.Deal.Bundle.SessionID)
	require.Equal(t, bundle.Signature, env.Deal.Bundle.Signature)
	require.Len(t, env.Deal.Bundle.Public, 2)
	require.True(t, env.Deal.Bundle.Public[0].Equal(pub0))
	require.True(t, env.Deal.Bundle.Public[1].Equal(pub1))
}

func TestEnvelope_Response_RoundTrip(t *testing.T) {
	suite := testSuite(t)

	bundle := &dkg.ResponseBundle{
		ShareIndex: 5,
		Responses: []dkg.Response{
			{DealerIndex: 1, Status: true},
			{DealerIndex: 2, Status: false},
		},
		SessionID: []byte("session-resp"),
		Signature: []byte("sig-resp"),
	}

	in := &ResponseEnvelope{
		ClusterID:  sampleClusterID(),
		Generation: 7,
		Bundle:     bundle,
	}

	wrapped, err := WrapResponse(in)
	require.NoError(t, err)
	require.Equal(t, byte(KindResponse), wrapped[1])

	env, err := Unwrap(wrapped, suite)
	require.NoError(t, err)
	require.Equal(t, KindResponse, env.Kind)
	require.NotNil(t, env.Response)
	require.Equal(t, in.ClusterID, env.Response.ClusterID)
	require.Equal(t, in.Generation, env.Response.Generation)
	require.Equal(t, bundle.ShareIndex, env.Response.Bundle.ShareIndex)
	require.Equal(t, bundle.Responses, env.Response.Bundle.Responses)
	require.Equal(t, bundle.SessionID, env.Response.Bundle.SessionID)
	require.Equal(t, bundle.Signature, env.Response.Bundle.Signature)
}

func TestEnvelope_Justification_RoundTrip(t *testing.T) {
	suite := testSuite(t)

	scalar0 := suite.Scalar().Pick(suite.RandomStream())
	scalar1 := suite.Scalar().Pick(suite.RandomStream())

	bundle := &dkg.JustificationBundle{
		DealerIndex: 2,
		Justifications: []dkg.Justification{
			{ShareIndex: 3, Share: scalar0},
			{ShareIndex: 4, Share: scalar1},
		},
		SessionID: []byte("session-just"),
		Signature: []byte("sig-just"),
	}

	in := &JustificationEnvelope{
		ClusterID:  sampleClusterID(),
		Generation: 7,
		Bundle:     bundle,
	}

	wrapped, err := WrapJustification(in)
	require.NoError(t, err)
	require.Equal(t, byte(KindJustification), wrapped[1])

	env, err := Unwrap(wrapped, suite)
	require.NoError(t, err)
	require.Equal(t, KindJustification, env.Kind)
	require.NotNil(t, env.Justification)
	require.Equal(t, in.ClusterID, env.Justification.ClusterID)
	require.Equal(t, in.Generation, env.Justification.Generation)
	require.Equal(t, bundle.DealerIndex, env.Justification.Bundle.DealerIndex)
	require.Equal(t, bundle.SessionID, env.Justification.Bundle.SessionID)
	require.Equal(t, bundle.Signature, env.Justification.Bundle.Signature)
	require.Len(t, env.Justification.Bundle.Justifications, 2)
	require.Equal(t, uint32(3), env.Justification.Bundle.Justifications[0].ShareIndex)
	require.True(t, env.Justification.Bundle.Justifications[0].Share.Equal(scalar0))
	require.Equal(t, uint32(4), env.Justification.Bundle.Justifications[1].ShareIndex)
	require.True(t, env.Justification.Bundle.Justifications[1].Share.Equal(scalar1))
}

// ---- error paths ------------------------------------------------------

func TestEnvelope_Truncated(t *testing.T) {
	_, err := Unwrap(nil, nil)
	require.Error(t, err)
	_, err = Unwrap([]byte{0x01}, nil) // version only, no kind
	require.Error(t, err)
}

func TestEnvelope_UnknownVersion(t *testing.T) {
	_, err := Unwrap([]byte{0xFF, byte(KindExchange)}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported envelope version")
}

func TestEnvelope_UnknownKind(t *testing.T) {
	_, err := Unwrap([]byte{EnvelopeVersionV1, 0xFE}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown envelope kind")
}

func TestEnvelope_DealRequiresSuite(t *testing.T) {
	// A well-formed Deal envelope cannot be decoded without a suite (the
	// public points need a suite to be deserialized).
	suite := testSuite(t)
	pub0 := suite.Point().Mul(suite.Scalar().Pick(suite.RandomStream()), nil)
	in := &DealEnvelope{
		ClusterID:  sampleClusterID(),
		Generation: 1,
		Bundle: &dkg.DealBundle{
			DealerIndex: 1,
			Public:      []kyber.Point{pub0},
			SessionID:   []byte("s"),
			Signature:   []byte("s"),
		},
	}
	wrapped, err := WrapDeal(in)
	require.NoError(t, err)

	_, err = Unwrap(wrapped, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-nil suite")
}

func TestEncodeExchange_Validates(t *testing.T) {
	_, err := EncodeExchange(nil)
	require.Error(t, err)
	_, err = EncodeExchange(&Exchange{}) // empty PubKey
	require.Error(t, err)
}

func TestDecodeExchange_BadClusterIDLen(t *testing.T) {
	// Hand-crafted JSON with a 16-byte cluster_id (should be 32).
	bad := []byte(`{"cluster_id":"00000000000000000000000000000000","operator_id":1,"pub_key":"aa"}`)
	_, err := DecodeExchange(bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "32 bytes")
}
