package qbft

import (
	"crypto/rsa"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// virtualOperatorSigner signs SSVMessages with the operator's RSA private key.
// Production signs the same way (operator's RSA key for outer-envelope auth);
// using real RSA keeps qbft.Instance's signature verification path live in our
// sims without bypass.
type virtualOperatorSigner struct {
	op spectypes.OperatorID
	sk *rsa.PrivateKey
}

func newVirtualOperatorSigner(op spectypes.OperatorID, sk *rsa.PrivateKey) *virtualOperatorSigner {
	return &virtualOperatorSigner{op: op, sk: sk}
}

func (s *virtualOperatorSigner) SignSSVMessage(msg *spectypes.SSVMessage) ([]byte, error) {
	return spectypes.SignSSVMessage(s.sk, msg)
}

func (s *virtualOperatorSigner) GetOperatorID() spectypes.OperatorID {
	return s.op
}
