package threshold

import (
	"strconv"

	"github.com/herumi/bls-eth-go-binary/bls"
)

type Polynomial struct {
	secret              bls.Fr
	Degree              uint32
	interpolationPoints [][]bls.Fr

	Coefficients []bls.Fr
}

func NewPolynomial(secret *bls.SecretKey, degree uint32) (*Polynomial, error) {
	ret := &Polynomial{
		secret:       *bls.CastFromSecretKey(secret),
		Degree:       degree,
		Coefficients: make([]bls.Fr, degree),
	}

	err := ret.GenerateRandom()
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func NewLagrangeInterpolation(points [][]bls.Fr) *Polynomial {
	return &Polynomial{
		interpolationPoints: points,
	}
}

func (p *Polynomial) GenerateRandom() error {
	p.Coefficients[0] = p.secret // important the free coefficient is in index 0
	for i := uint32(1); i < p.Degree; i++ {
		sk := bls.Fr{}
		sk.SetByCSPRNG()
		p.Coefficients[i] = sk
	}
	return nil
}

func (p *Polynomial) toString() string {
	ret := "y = "
	for i := int(p.Degree) - 1; i >= 0; i-- {
		ret += p.Coefficients[i].GetString(10) + "x^" + strconv.Itoa(int(i)) + " "
		if i > 0 {
			ret += "+ "
		}
	}
	return ret
}

func (p *Polynomial) EvaluateUint64(pointUint64 uint64) (*bls.Fr, error) {
	point := &bls.Fr{}
	point.SetInt64(int64(pointUint64))
	return p.Evaluate(point)
}

func (p *Polynomial) Evaluate(point *bls.Fr) (*bls.Fr, error) {
	res := &bls.Fr{}

	err := bls.FrEvaluatePolynomial(res, p.Coefficients, point)
	if err != nil {
		return nil, err
	}

	return res, nil
}

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
