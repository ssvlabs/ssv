package threshold

import (
	"fmt"
	"math/big"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/bloxapp/ssv/protocol/v1/message"
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
func Create(skBytes []byte, threshold uint64, count uint64) (map[message.OperatorID]*bls.SecretKey, error) {
	// master key Polynomial
	msk := make([]bls.SecretKey, threshold)

	sk := &bls.SecretKey{}
	if err := sk.Deserialize(skBytes); err != nil {
		return nil, err
	}
	msk[0] = *sk

	// construct poly
	for i := uint64(1); i < threshold; i++ {
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()
		msk[i] = sk
	}

	// evaluate shares - starting from 1 because 0 is master key
	shares := make(map[message.OperatorID]*bls.SecretKey)
	for i := uint64(1); i <= count; i++ {
		blsID := bls.ID{}

		err := blsID.SetDecString(fmt.Sprintf("%d", i))
		if err != nil {
			return nil, err
		}

		sk := bls.SecretKey{}

		err = sk.Set(msk, &blsID)
		if err != nil {
			return nil, err
		}

		shares[message.OperatorID(i)] = &sk
	}
	return shares, nil
}
