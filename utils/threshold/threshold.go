package threshold

import (
	"math/big"

	"github.com/herumi/bls-eth-go-binary/bls"
)

var (
	curveOrder = new(big.Int)
)

func Init() {
	bls.Init(bls.BLS12_381)
	bls.SetETHmode(bls.EthModeDraft07)

	curveOrder, _ = curveOrder.SetString(bls.GetCurveOrder(), 10)
}

// TODO - use bls' SSS functions, no need to re-implement
// TODO - Create should receive a slice of indexes as well
// Create receives a bls.SecretKey hex and count.
// Will split the secret key into count shares
func Create(skBytes []byte, count uint64) ([]*bls.SecretKey, error) {
	ret := make([]*bls.SecretKey, 0)

	// construct poly
	sk := &bls.SecretKey{}
	sk.Deserialize(skBytes)
	p, err := NewPolynomial(sk, uint32(count-1))
	if err != nil {
		return nil, err
	}

	// evaluate
	for i := uint64(1); i <= count; i++ { // we can't split share on index 0
		shareFr, err := p.EvaluateUint64(i)
		if err != nil {
			return nil, err
		}
		ret = append(ret, bls.CastToSecretKey(shareFr))
	}

	return ret, nil
}
