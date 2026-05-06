package blsbackend

import (
	"errors"
	"fmt"

	"github.com/drand/kyber"
	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/pairing"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// KyberSigner is a obft.Signer backed by drand's kyber-bls12381. It exists
// alongside BLSSigner (herumi-backed): operators can use the SAME secret
// share with both signers, producing signatures with different DSTs
// (Eth2 DST via herumi, drand's NUL DST via kyber).
//
// This is the "DST-trick" approach (see docs/IBE-INTEGRATION.md): the
// herumi-generated validator share serves both validator-output signing
// and IBE-tag signing. No separate DKG is required.
//
// A KyberSigner is bound to one operator's secret share at construction —
// the share never travels through the call stack as a parameter. As with
// BLSSigner, aggregation and verification are stateless wrt the bound
// share and work on any KyberSigner instance.
//
// Wire format for partial / aggregate signatures: kyber's G2 compressed
// point bytes (96 bytes). The IBE decryption path takes the aggregated
// signature bytes, parses them back into a kyber G2 point, and uses it
// as the decryption key.
//
// All inputs and outputs follow the same `obft.Signer` semantics as
// `BLSSigner`:
//
//   - share        : serialized herumi BLS secret-key bytes (32 bytes)
//   - pubKeyShare  : serialized herumi BLS public-key bytes (48 bytes, G1)
//   - clusterPubKey: serialized herumi BLS master pubkey bytes (48 bytes, G1)
//   - msg          : arbitrary bytes
//   - partial / sig: kyber G2 compressed point bytes (96 bytes)
type KyberSigner struct {
	suite pairing.Suite
	// share is the herumi-format BLS secret-share bytes the signer is
	// bound to (interpreted as a kyber scalar via HerumiShareToKyberScalar).
	// May be empty for verify-only / aggregate-only use cases (SignPartial
	// then returns an error).
	share []byte
}

// NewKyberSigner constructs a KyberSigner bound to `share`, using
// kyber-bls12381's default suite (with default DSTs — drand's NUL DST for
// G2). `share` may be nil/empty for verify-only / aggregate-only use cases.
func NewKyberSigner(share []byte) *KyberSigner {
	out := &KyberSigner{suite: bls12381.NewBLS12381Suite()}
	if len(share) > 0 {
		out.share = append([]byte(nil), share...)
	}
	return out
}

// SignPartial computes `share · H_G2(msg)` using kyber's hash-to-G2 with
// drand's DST, where `share` is the bound share from construction. Returns
// the kyber G2 compressed point bytes.
func (k *KyberSigner) SignPartial(msg []byte) (obft.Signature, error) {
	if len(k.share) == 0 {
		return nil, errors.New("blsbackend: KyberSigner has no share bound (verify-only)")
	}
	if len(msg) == 0 {
		return nil, errors.New("blsbackend: KyberSigner: empty message")
	}
	scalar, err := HerumiShareToKyberScalar(k.share)
	if err != nil {
		return nil, err
	}

	hashable, ok := k.suite.G2().Point().(kyber.HashablePoint)
	if !ok {
		return nil, errors.New("blsbackend: kyber G2 point doesn't implement HashablePoint")
	}
	msgPoint := hashable.Hash(msg)
	sig := k.suite.G2().Point().Mul(scalar, msgPoint)

	out, err := sig.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("blsbackend: marshal kyber G2 sig: %w", err)
	}
	return obft.Signature(out), nil
}

// indexedPoint pairs an operator-ID x-coordinate with a kyber G2 point
// (the partial signature). Used during Lagrange interpolation.
type indexedPoint struct {
	x  uint64
	pt kyber.Point
}

// AggregatePartials Lagrange-interpolates G2 partial signatures. The map
// keys (operator IDs) are the x-coordinates; each partial is treated as
// the y-coordinate at x=opID. The result is the polynomial value at
// x=0, i.e. the master signature `s · H_G2(msg)`.
//
// Any subset of size ≥ q (where q is the threshold) yields the SAME
// result regardless of which operators are in the subset — Lagrange
// interpolation is a property of the field, library-independent.
func (k *KyberSigner) AggregatePartials(partials map[obft.OperatorID]obft.Signature) (obft.Signature, error) {
	if len(partials) == 0 {
		return nil, errors.New("blsbackend: KyberSigner: no partials to aggregate")
	}

	pts := make([]indexedPoint, 0, len(partials))
	for opID, sig := range partials {
		pt := bls12381.NullKyberG2()
		if err := pt.UnmarshalBinary(sig); err != nil {
			return nil, fmt.Errorf("blsbackend: unmarshal partial for op %d: %w", opID, err)
		}
		pts = append(pts, indexedPoint{x: uint64(opID), pt: pt})
	}

	// Compute Lagrange basis coefficients ℓ_j(0) for each j, multiply each
	// partial by its coefficient, and sum.
	result := k.suite.G2().Point().Null()
	for _, idx := range pts {
		coeff := lagrangeCoeffAtZero(k.suite, idx.x, pts)
		term := k.suite.G2().Point().Mul(coeff, idx.pt)
		result = result.Add(result, term)
	}

	out, err := result.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("blsbackend: marshal aggregate G2 sig: %w", err)
	}
	return obft.Signature(out), nil
}

// VerifyPartial checks that `partial = pubKeyShareScalar · H_G2(msg)` via
// the BLS pairing equation: e(G1, partial) == e(pubKeyShare, H_G2(msg)).
//
// `pubKeyShare` is the operator's public-key share in herumi-serialized
// G1 format (48 bytes). It's converted to kyber-G1 internally.
func (k *KyberSigner) VerifyPartial(pubKeyShare []byte, msg []byte, partial obft.Signature) bool {
	if len(msg) == 0 || len(partial) == 0 {
		return false
	}
	pubPoint, err := HerumiPubkeyToKyberG1Point(pubKeyShare)
	if err != nil {
		return false
	}
	sigPoint := bls12381.NullKyberG2()
	if err := sigPoint.UnmarshalBinary(partial); err != nil {
		return false
	}

	hashable, ok := k.suite.G2().Point().(kyber.HashablePoint)
	if !ok {
		return false
	}
	msgPoint := hashable.Hash(msg)

	return k.suite.ValidatePairing(k.suite.G1().Point().Base(), sigPoint, pubPoint, msgPoint)
}

// VerifyAggregate is identical in shape to VerifyPartial — BLS verification
// doesn't distinguish "partial" from "full" signatures at the math level.
// The difference is contextual: callers pass the master pubkey (cluster
// pubkey) here and an operator's pubkey share to VerifyPartial.
func (k *KyberSigner) VerifyAggregate(clusterPubKey []byte, msg []byte, sig obft.Signature) bool {
	return k.VerifyPartial(clusterPubKey, msg, sig)
}

// lagrangeCoeffAtZero computes ℓ_j(0) = ∏_{m≠j} (-x_m) / (x_j - x_m) over
// the kyber scalar field. `xj` is the x-coordinate of the share whose
// coefficient we're computing; `pts` is all the (x, point) pairs in the
// aggregation.
func lagrangeCoeffAtZero(suite pairing.Suite, xj uint64, pts []indexedPoint) kyber.Scalar {
	num := suite.G2().Scalar().One()
	den := suite.G2().Scalar().One()
	// Operator IDs (the x-coordinates) are well within int64 range — they're
	// 1-indexed cluster positions in practice (≤ tens at the absolute extreme).
	xjScalar := suite.G2().Scalar().SetInt64(int64(xj)) //nolint:gosec // operator id

	for _, idx := range pts {
		if idx.x == xj {
			continue
		}
		xmScalar := suite.G2().Scalar().SetInt64(int64(idx.x)) //nolint:gosec // operator id
		negXm := suite.G2().Scalar().Neg(xmScalar)
		num = num.Mul(num, negXm)
		diff := suite.G2().Scalar().Sub(xjScalar, xmScalar)
		den = den.Mul(den, diff)
	}
	denInv := suite.G2().Scalar().Inv(den)
	return suite.G2().Scalar().Mul(num, denInv)
}
