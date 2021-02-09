package threshold

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
)

func frFromInt(i int64) bls.Fr {
	p := bls.Fr{}
	p.SetInt64(i)
	return p
}

func frPointerFromInt(i int64) *bls.Fr {
	p := &bls.Fr{}
	p.SetInt64(i)
	return p
}

func TestEvaluation(t *testing.T) {
	Init()

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()
	degree := 3

	p, err := NewPolynomial(sk, uint32(degree))
	require.NoError(t, err)
	err = p.GenerateRandom()
	require.NoError(t, err)

	//fmt.Printf("%s\n", p.toString())

	res1, err := p.Evaluate(frPointerFromInt(1))
	require.NoError(t, err)
	res2, err := p.Evaluate(frPointerFromInt(2))
	require.NoError(t, err)
	res3, err := p.Evaluate(frPointerFromInt(3))
	require.NoError(t, err)
	res4, err := p.Evaluate(frPointerFromInt(4))
	require.NoError(t, err)

	// Interpolate back
	points := [][]bls.Fr{
		{frFromInt(1), *res1},
		{frFromInt(2), *res2},
		{frFromInt(3), *res3},
		{frFromInt(4), *res4},
	}
	pInter := NewLagrangeInterpolation(points)
	res, err := pInter.Interpolate()
	require.NoError(t, err)

	require.Equal(t, bls.CastFromSecretKey(sk).GetString(10), res.GetString(10))
}

func TestInterpolation(t *testing.T) {
	Init()

	points := [][]bls.Fr{
		{frFromInt(1), frFromInt(7)},
		{frFromInt(2), frFromInt(10)},
		{frFromInt(3), frFromInt(15)},
	}

	p := NewLagrangeInterpolation(points)
	res, err := p.Interpolate()
	require.NoError(t, err)

	require.Equal(t, "6", res.GetString(10))
}
