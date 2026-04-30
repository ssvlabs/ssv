package blsbackend

import (
	"fmt"

	"github.com/drand/kyber"
	bls12381 "github.com/drand/kyber-bls12381"
)

// Conversions between herumi/bls-eth-go-binary serialized bytes and
// kyber-bls12381 typed values.
//
// The cryptographic key material is identical across the two libraries —
// both operate on BLS12-381 scalars and curve points. What differs is:
//
//   - The hash-to-curve Domain Separation Tag (DST). herumi (in EthMode) uses
//     "BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_"; kyber-bls12381's default
//     uses "BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_NUL_". This means herumi-
//     produced signatures and kyber-produced signatures over the same message
//     are different curve points — by design.
//
//   - The byte encoding of scalars and points. Both libraries should be
//     byte-compatible with the IETF BLS12-381 standard, but the conversion
//     functions here defensively assert the expected lengths and format.
//
// The "DST trick" used in this codebase: a herumi BLS secret share — already
// generated and held by the operator for validator-share signing — can be
// directly used as a kyber-BLS scalar to produce signatures under kyber's
// DST. Since the scalar is the same and DSTs differ, the two signatures are
// cryptographically independent (cross-protocol secure), but Lagrange
// interpolation over kyber-format partial sigs from 2f+1 operators yields
// a valid full BLS signature usable as a tlock IBE decryption key — without
// any new DKG ceremony. See docs/IBE-INTEGRATION.md.

// HerumiSecretShareSize is the number of bytes herumi's bls.SecretKey.Serialize()
// produces for a BLS12-381 share. (BLS12-381 scalar field is ~256 bits; 32 bytes.)
const HerumiSecretShareSize = 32

// HerumiPubkeyG1Size is the number of bytes herumi's bls.PublicKey.Serialize()
// produces in Eth2's min-pk variant (compressed G1 point, 48 bytes).
const HerumiPubkeyG1Size = 48

// HerumiShareToKyberScalar reinterprets a herumi-serialized BLS secret share
// as a kyber-bls12381 Scalar. herumi serializes scalars as 32-byte big-endian
// integers reduced modulo the curve order; kyber's mod.Int.SetBytes also
// interprets bytes big-endian and reduces modulo the curve order. This is a
// direct pass-through with a length check.
//
// Returns an error if the input is not 32 bytes.
func HerumiShareToKyberScalar(shareBytes []byte) (kyber.Scalar, error) {
	if len(shareBytes) != HerumiSecretShareSize {
		return nil, fmt.Errorf("blsbackend: expected %d-byte herumi share, got %d",
			HerumiSecretShareSize, len(shareBytes))
	}
	s := bls12381.NewKyberScalar()
	s.SetBytes(shareBytes)
	return s, nil
}

// HerumiPubkeyToKyberG1Point reinterprets a herumi-serialized BLS public key
// (compressed G1 point in Eth2 min-pk format) as a kyber-bls12381 G1 Point.
// Both libraries follow the IETF/Eth2 compressed-point encoding for BLS12-381,
// so this is a direct pass-through with a length check.
//
// Returns an error if the input is not 48 bytes or doesn't decode as a valid
// G1 point.
func HerumiPubkeyToKyberG1Point(pubBytes []byte) (kyber.Point, error) {
	if len(pubBytes) != HerumiPubkeyG1Size {
		return nil, fmt.Errorf("blsbackend: expected %d-byte herumi pubkey, got %d",
			HerumiPubkeyG1Size, len(pubBytes))
	}
	p := bls12381.NullKyberG1()
	if err := p.UnmarshalBinary(pubBytes); err != nil {
		return nil, fmt.Errorf("blsbackend: unmarshal herumi pubkey as kyber G1: %w", err)
	}
	return p, nil
}
