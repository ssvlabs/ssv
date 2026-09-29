package gloas

import (
	"encoding/json"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

// builderSpecsSignedRequestAuthJSON is the wire example from builder-specs
// examples/gloas/signed_request_auth.json (the OpenAPI example's value field).
const builderSpecsSignedRequestAuthJSON = `{
  "message": {
    "data": "0x1234567890abcdef",
    "slot": "1"
  },
  "signature": "0x1b66ac1fb663c9bc59509846d6ec05345bd908eda73e670af888da41af171505cc411d61252fb6cb3fa0017b679f8bb2305b26a285fa2737f175668d0dff91cc1b66ac1fb663c9bc59509846d6ec05345bd908eda73e670af888da41af171505"
}`

func TestBuilderRequestAuth_SSZ(t *testing.T) {
	r := &BuilderRequestAuth{
		Data: []byte("https://builder.example.com"),
		Slot: 42,
	}
	// 4-byte data offset + 8-byte slot + data tail.
	require.Equal(t, 12+len(r.Data), r.SizeSSZ())

	b, err := r.MarshalSSZ()
	require.NoError(t, err)
	require.Len(t, b, 12+len(r.Data))

	var dec BuilderRequestAuth
	require.NoError(t, dec.UnmarshalSSZ(b))
	require.Equal(t, r, &dec)

	htr1, err := r.HashTreeRoot()
	require.NoError(t, err)
	htr2, err := dec.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, htr1, htr2)
}

func TestBuilderRequestAuth_SSZ_DataLimit(t *testing.T) {
	// At the ByteList limit both directions succeed.
	atLimit := &BuilderRequestAuth{Data: make([]byte, MaxBuilderAuthDataSize), Slot: 1}
	b, err := atLimit.MarshalSSZ()
	require.NoError(t, err)
	var dec BuilderRequestAuth
	require.NoError(t, dec.UnmarshalSSZ(b))
	require.Len(t, dec.Data, MaxBuilderAuthDataSize)

	// One byte over: marshal of the oversize object and unmarshal of an oversize tail both fail.
	over := &BuilderRequestAuth{Data: make([]byte, MaxBuilderAuthDataSize+1), Slot: 1}
	_, err = over.MarshalSSZ()
	require.Error(t, err)
	oversize := append(b, 0x00)
	require.Error(t, dec.UnmarshalSSZ(oversize))
}

// TestBuilderRequestAuth_HashTreeRoot_Golden pins the merkleization against roots computed with an
// independent implementation (plain SHA-256 chunk merkleization of the builder-specs layout:
// ByteList[4096] → 128 chunk leaves + length mix-in, then 2-leaf container).
func TestBuilderRequestAuth_HashTreeRoot_Golden(t *testing.T) {
	auth := &BuilderRequestAuth{Data: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef}, Slot: 1}
	htr, err := auth.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t,
		"0x3d6f2e216a08828a2bc2c9644edacd56fef00f2be824abc49f6d1a28569bd880",
		phase0.Root(htr).String())

	empty := &BuilderRequestAuth{Slot: 1}
	htr, err = empty.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t,
		"0xa5b4c560790a4fbfd24ad385f1353d605987bbbf53549e314241a3093986e773",
		phase0.Root(htr).String())
}

func TestSignedBuilderRequestAuth_SSZ(t *testing.T) {
	s := &SignedBuilderRequestAuth{
		Message:   &BuilderRequestAuth{Data: []byte("token"), Slot: 9},
		Signature: phase0.BLSSignature{0xbb, 0xcc},
	}
	// 4-byte message offset + 96-byte signature + message tail.
	require.Equal(t, 100+12+len(s.Message.Data), s.SizeSSZ())

	b, err := s.MarshalSSZ()
	require.NoError(t, err)

	var dec SignedBuilderRequestAuth
	require.NoError(t, dec.UnmarshalSSZ(b))
	require.Equal(t, s, &dec)
}

// TestSignedBuilderRequestAuth_BuilderSpecsExample decodes the builder-specs wire example and pins both
// the field values and the hash tree root (computed with an independent implementation).
func TestSignedBuilderRequestAuth_BuilderSpecsExample(t *testing.T) {
	var s SignedBuilderRequestAuth
	require.NoError(t, json.Unmarshal([]byte(builderSpecsSignedRequestAuthJSON), &s))
	require.Equal(t, []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef}, s.Message.Data)
	require.Equal(t, phase0.Slot(1), s.Message.Slot)

	htr, err := s.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t,
		"0xed625cfd05f57d635b2f64d57cdcf3b3a3ba2dfece169c5602b31ab62884683e",
		phase0.Root(htr).String())

	// Re-marshaling reproduces the example byte-for-byte up to JSON formatting.
	out, err := json.Marshal(&s)
	require.NoError(t, err)
	require.JSONEq(t, builderSpecsSignedRequestAuthJSON, string(out))
}

func TestBuilderRequestAuth_JSON(t *testing.T) {
	r := &BuilderRequestAuth{Data: []byte("https://builder.example.com"), Slot: 123}
	out, err := json.Marshal(r)
	require.NoError(t, err)

	var dec BuilderRequestAuth
	require.NoError(t, json.Unmarshal(out, &dec))
	require.Equal(t, r, &dec)

	// Empty data round-trips as "0x".
	var empty BuilderRequestAuth
	require.NoError(t, json.Unmarshal([]byte(`{"data":"0x","slot":"7"}`), &empty))
	require.Empty(t, empty.Data)
	require.Equal(t, phase0.Slot(7), empty.Slot)

	// Oversize data, malformed hex, and malformed slot are rejected.
	oversize := make([]byte, MaxBuilderAuthDataSize+1)
	oversizeJSON, err := json.Marshal(&BuilderRequestAuth{Data: oversize})
	require.NoError(t, err)
	require.ErrorContains(t, json.Unmarshal(oversizeJSON, &dec), "incorrect length for data")
	require.ErrorContains(t, json.Unmarshal([]byte(`{"data":"0xzz","slot":"1"}`), &dec), "invalid value for data")
	require.ErrorContains(t, json.Unmarshal([]byte(`{"data":"0x00","slot":"x"}`), &dec), "invalid value for slot")
}

func TestSignedBuilderRequestAuth_JSON_MessageMissing(t *testing.T) {
	var s SignedBuilderRequestAuth
	require.ErrorContains(t, json.Unmarshal([]byte(`{"signature":"0x00"}`), &s), "message missing")
}
