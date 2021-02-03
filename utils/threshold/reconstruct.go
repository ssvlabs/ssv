package threshold

import (
	"math/big"

	"github.com/herumi/bls-eth-go-binary/bls"
)

// ReconstructSignatures reconstructs the given signatures done by threshold private keys
func ReconstructSignatures(signatures [][]byte) ([]byte, error) {
	var r bls.G2
	for i, signature := range signatures {
		var sigPoint bls.G2
		if err := sigPoint.Deserialize(signature); err != nil {
			return nil, err
		}

		coef := big.NewInt(1)
		for j := range signatures {
			if j == i {
				continue
			}

			coef := coef.Mul(new(big.Int).Neg(coef), big.NewInt(int64(j+1)))
			coef = coef.Mul(coef, new(big.Int).Mod(new(big.Int).ModInverse(big.NewInt(int64(i-j)), curveOrder), curveOrder))
		}

		// TODO: Implement
	}

	return r.Serialize(), nil
}
