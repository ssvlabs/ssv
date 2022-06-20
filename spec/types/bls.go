package types

import (
	"github.com/herumi/bls-eth-go-binary/bls"
	"math/big"
)

var (
	curveOrder = new(big.Int)
)

// InitBLS initializes BLS
func InitBLS() {
	_ = bls.Init(bls.BLS12_381)
	_ = bls.SetETHmode(bls.EthModeDraft07)

	curveOrder, _ = curveOrder.SetString(bls.GetCurveOrder(), 10)
}
