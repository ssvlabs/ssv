package threshold

import (
	"strconv"

	"github.com/herumi/bls-eth-go-binary/bls"
)

// Polynomial implements polynomial related logic
type Polynomial struct {
	secret              bls.Fr
	Degree              uint32
	interpolationPoints [][]bls.Fr

	Coefficients []bls.Fr
}

// NewPolynomial is the constructor of Polynomial
func NewPolynomial(secret *bls.SecretKey, degree uint32) (*Polynomial, error) {
	ret := &Polynomial{
		secret:       *bls.CastFromSecretKey(secret),
		Degree:       degree,
		Coefficients: make([]bls.Fr, degree),
	}

	if err := ret.GenerateRandom(); err != nil {
		return nil, err
	}

	return ret, nil
}

// NewLagrangeInterpolation is the constructor of Polynomial
func NewLagrangeInterpolation(points [][]bls.Fr) *Polynomial {
	return &Polynomial{
		interpolationPoints: points,
	}
}

// GenerateRandom generates random polynomial
func (p *Polynomial) GenerateRandom() error {
	p.Coefficients[0] = p.secret // important the free coefficient is in index 0
	for i := uint32(1); i < p.Degree; i++ {
		sk := bls.Fr{}
		sk.SetByCSPRNG()
		p.Coefficients[i] = sk
	}
	return nil
}

// toString converts polynomial to string
//nolint:unused
func (p *Polynomial) toString() string {
	ret := "y = "
	for i := int(p.Degree) - 1; i >= 0; i-- {
		ret += p.Coefficients[i].GetString(10) + "x^" + strconv.Itoa(i) + " "
		if i > 0 {
			ret += "+ "
		}
	}
	return ret
}

// EvaluateUint64 evaluates the given uint64 value
func (p *Polynomial) EvaluateUint64(pointUint64 uint64) (*bls.Fr, error) {
	point := &bls.Fr{}
	point.SetInt64(int64(pointUint64))
	return p.Evaluate(point)
}

// Evaluate evaluates the given Fr point
func (p *Polynomial) Evaluate(point *bls.Fr) (*bls.Fr, error) {
	res := &bls.Fr{}

	err := bls.FrEvaluatePolynomial(res, p.Coefficients, point)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// Interpolate interpolates polynomial and returns Fr point
func (p *Polynomial) Interpolate() (*bls.Fr, error) {
	x := make([]bls.Fr, len(p.interpolationPoints))
	y := make([]bls.Fr, len(p.interpolationPoints))
	for i := range p.interpolationPoints {
		point := p.interpolationPoints[i]
		x[i] = point[0]
		y[i] = point[1]
	}

	res := &bls.Fr{}
	err := bls.FrLagrangeInterpolation(res, x, y)
	if err != nil {
		return nil, err
	}
	return res, nil
}
