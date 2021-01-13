package threshold

import (
	"math/big"
	"math/rand"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var (
	curveOrder = new(big.Int)
)

func init() {
	bls.Init(bls.BLS12_381)
	bls.SetETHmode(bls.EthModeDraft07)

	curveOrder, _ = curveOrder.SetString(bls.GetCurveOrder(), 10)
}

// Create turns the given private key into a threshold key
func Create(privKeyHex string, count uint64) ([]*bls.SecretKey, error) {
	privKeyInt := hexutil.MustDecodeBig("0x" + privKeyHex)
	coefs := []*big.Int{privKeyInt}
	for i := 0; i < 2; i++ {
		randVal := new(big.Int)
		randVal = randVal.Rand(rand.New(rand.NewSource(time.Now().UnixNano())), curveOrder)
		coefs = append(coefs, randVal)
	}

	privKeys := []*bls.SecretKey{}
	for i := uint64(1); i <= count; i++ {
		prkInt := evalPoly(coefs, i)
		pkrRaw := hexutil.EncodeBig(prkInt)
		sk := &bls.SecretKey{}
		if err := sk.SetHexString(pkrRaw); err != nil {
			return nil, err
		}
		privKeys = append(privKeys, sk)
	}

	return privKeys, nil
}

func evalPoly(coeffs []*big.Int, x uint64) *big.Int {
	result := big.NewInt(0)
	powerOfX := big.NewInt(1)
	xInt := big.NewInt(int64(x))
	for i := 0; i < len(coeffs); i++ {
		val := coeffs[i]
		val = val.Mul(val, powerOfX)
		val = val.Mod(val, curveOrder)

		result = result.Add(result, val)

		powerOfX = powerOfX.Mul(powerOfX, xInt)
		powerOfX = powerOfX.Mod(powerOfX, curveOrder)
	}
	return result.Mod(result, curveOrder)
}
