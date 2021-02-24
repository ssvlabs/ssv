package threshold

import (
	"math/big"

	"github.com/herumi/bls-eth-go-binary/bls"
)

var (
	curveOrder = new(big.Int)
)

// Init initializes BLS
func Init() {
	_ = bls.Init(bls.BLS12_381)
	_ = bls.SetETHmode(bls.EthModeDraft07)

	curveOrder, _ = curveOrder.SetString(bls.GetCurveOrder(), 10)
}

// Create receives a bls.SecretKey hex and count.
// Will split the secret key into count shares
// TODO - use bls' SSS functions, no need to re-implement
func Create(skBytes []byte, count uint64) (map[uint64]*bls.SecretKey, error) {
	sk := &bls.SecretKey{}
	if err := sk.Deserialize(skBytes); err != nil {
		return nil, err
	}

	// construct poly
	p, err := NewPolynomial(sk, uint32(count-1))
	if err != nil {
		return nil, err
	}

	// evaluate
	ret := make(map[uint64]*bls.SecretKey, 0)
	for i := uint64(1); i <= count; i++ { // we can't split share on index 0
		shareFr, err := p.EvaluateUint64(i)
		if err != nil {
			return nil, err
		}
		ret[i] = bls.CastToSecretKey(shareFr)
	}

	return ret, nil
}
