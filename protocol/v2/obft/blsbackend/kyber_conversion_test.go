package blsbackend

import (
	"testing"

	"github.com/drand/kyber"
	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/utils/threshold"
)

// The DST-trick approach hinges on byte-format
// compatibility between herumi/bls-eth-go-binary and kyber-bls12381 for:
//
//   1. BLS12-381 scalars (32 bytes, big-endian) — for sharing operator
//      secret keys across libraries.
//   2. BLS12-381 G1 compressed points (48 bytes) — for sharing public keys.
//
// Both libraries are supposed to follow the IETF / Eth2 standard encoding,
// but compatibility is empirical and worth verifying directly. These tests
// are the foundation for KyberSigner and TLockIBE: if they fail, the entire
// "reuse herumi shares for kyber-side IBE" plan needs a byte-format
// conversion shim.

func TestHerumiShareBytesParseAsKyberScalar(t *testing.T) {
	threshold.Init()

	// Generate a random herumi BLS secret key.
	herumiSK := &bls.SecretKey{}
	herumiSK.SetByCSPRNG()
	herumiBytes := herumiSK.Serialize()
	require.Len(t, herumiBytes, HerumiSecretShareSize,
		"herumi secret key should serialize to %d bytes", HerumiSecretShareSize)

	// Convert to kyber scalar.
	kyberScalar, err := HerumiShareToKyberScalar(herumiBytes)
	require.NoError(t, err)
	require.NotNil(t, kyberScalar)

	// Round-trip: serialize the kyber scalar back; bytes should match
	// (modulo the high-bit normalisation kyber may apply for canonical
	// encoding, but for a uniformly-random scalar this should be byte-equal).
	kyberBytes, err := kyberScalar.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, herumiBytes, kyberBytes,
		"herumi share bytes should round-trip through kyber scalar")
}

func TestHerumiPubkeyBytesParseAsKyberG1Point(t *testing.T) {
	threshold.Init()

	herumiSK := &bls.SecretKey{}
	herumiSK.SetByCSPRNG()
	herumiPK := herumiSK.GetPublicKey()
	herumiBytes := herumiPK.Serialize()
	require.Len(t, herumiBytes, HerumiPubkeyG1Size,
		"herumi pubkey should serialize to %d bytes", HerumiPubkeyG1Size)

	kyberPoint, err := HerumiPubkeyToKyberG1Point(herumiBytes)
	require.NoError(t, err)
	require.NotNil(t, kyberPoint)

	// Round-trip: marshal back and compare.
	kyberBytes, err := kyberPoint.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, herumiBytes, kyberBytes,
		"herumi pubkey bytes should round-trip through kyber G1 point")
}

func TestHerumiPubkeyMatchesScalarTimesG1Generator(t *testing.T) {
	// Property: kyber-converted herumi pubkey = (herumi-scalar) * G1.
	// I.e. the same secret produces the same public key in either library
	// when both compute pubkey = sk * G1.
	threshold.Init()
	herumiSK := &bls.SecretKey{}
	herumiSK.SetByCSPRNG()
	herumiPK := herumiSK.GetPublicKey()

	kyberScalar, err := HerumiShareToKyberScalar(herumiSK.Serialize())
	require.NoError(t, err)
	kyberPubFromHerumi, err := HerumiPubkeyToKyberG1Point(herumiPK.Serialize())
	require.NoError(t, err)

	// Compute (kyberScalar) * G1 in kyber and compare.
	suite := bls12381.NewBLS12381Suite()
	kyberPubFromScalar := suite.G1().Point().Mul(kyberScalar, nil)

	// Both should be equal.
	require.True(t, kyberPubFromHerumi.Equal(kyberPubFromScalar),
		"herumi pubkey converted to kyber should equal kyberScalar * G1")
}

func TestHerumiThresholdShares_KyberLagrangeRecoversMaster(t *testing.T) {
	// Property: Lagrange interpolation of 2f+1 herumi shares — interpreted
	// as kyber scalars — recovers the master scalar (which is herumi's
	// master sk converted to kyber). This is the core of the DST-trick:
	// the threshold scheme holds in the scalar field, library-independent.
	threshold.Init()
	const n, q = 7, 5

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterScalar, err := HerumiShareToKyberScalar(master.Serialize())
	require.NoError(t, err)

	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	// Take any 5 of the 7 shares; reconstruct the master scalar in kyber
	// via Lagrange interpolation.
	subset := []uint64{1, 2, 3, 4, 5}
	suite := bls12381.NewBLS12381Suite()
	kyberG := suite.G1()

	scalars := make(map[uint64]kyber.Scalar, len(subset))
	for _, id := range subset {
		s, err := HerumiShareToKyberScalar(shares[id].Serialize())
		require.NoError(t, err)
		scalars[id] = s
	}

	recovered := lagrangeInterpolateKyber(scalars, kyberG.Scalar())
	require.True(t, masterScalar.Equal(recovered),
		"Lagrange-interpolated kyber scalar should equal the master scalar")
}

// lagrangeInterpolateKyber computes the Lagrange interpolation of `points`
// at x=0, in the kyber scalar field. Each entry's key is the x-coordinate
// (operator ID); values are the y-coordinates (kyber scalars). zero is a
// fresh kyber scalar used as a working register.
func lagrangeInterpolateKyber(points map[uint64]kyber.Scalar, zero kyber.Scalar) kyber.Scalar {
	result := zero.Clone().Zero()

	for j, yj := range points {
		// Compute Lagrange basis ℓ_j(0) = ∏_{m≠j} (-x_m) / (x_j - x_m).
		num := zero.Clone().One()
		den := zero.Clone().One()
		xj := zero.Clone().SetInt64(int64(j))
		for m := range points {
			if m == j {
				continue
			}
			xm := zero.Clone().SetInt64(int64(m))
			negXm := zero.Clone().Neg(xm)
			num = num.Mul(num, negXm)
			diff := zero.Clone().Sub(xj, xm)
			den = den.Mul(den, diff)
		}
		// ℓ_j(0) = num / den
		denInv := zero.Clone().Inv(den)
		basis := zero.Clone().Mul(num, denInv)
		// Add yj * ℓ_j(0) to result.
		term := zero.Clone().Mul(yj, basis)
		result = result.Add(result, term)
	}
	return result
}
