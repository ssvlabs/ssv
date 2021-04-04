package threshold

import (
	"github.com/herumi/bls-eth-go-binary/bls"
)

// ReconstructSignatures receives a map of user indexes and serialized bls.Sign.
// It then reconstructs the original threshold signature using lagrange interpolation
func ReconstructSignatures(signatures map[uint64][]byte) (*bls.Sign, error) {
	// prepare x and y coordinates
	x := make([]bls.Fr, 0)
	y := make([]bls.G2, 0)
	for index, signature := range signatures {
		var sigPoint bls.G2
		if err := sigPoint.Deserialize(signature); err != nil {
			return nil, err
		}

		point := bls.Fr{}
		point.SetInt64(int64(index))
		x = append(x, point)
		y = append(y, sigPoint)
	}

	// reconstruct
	l := ECCG2Polynomial{
		G2Points: y,
		XPoints:  x,
	}
	g2Point, err := l.Interpolate()
	if err != nil {
		return nil, err
	}
	return bls.CastToSign(g2Point), nil
}

// ECCG2Polynomial represents ECC G2 polynomial
type ECCG2Polynomial struct {
	G2Points []bls.G2
	XPoints  []bls.Fr
}

// Interpolate interpolates polynomial and returns G2
func (p *ECCG2Polynomial) Interpolate() (*bls.G2, error) {
	res := &bls.G2{}
	err := bls.G2LagrangeInterpolation(res, p.XPoints, p.G2Points)
	if err != nil {
		return nil, err
	}
	return res, nil
}
