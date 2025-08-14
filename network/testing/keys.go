package testing

import (
	"crypto/ecdsa"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

// NodeKeys holds node's keys
type NodeKeys struct {
	NetKey      *ecdsa.PrivateKey
	OperatorKey keys.OperatorPrivateKey
}

// CreateKeys creates <n> random node keys
func CreateKeys(n int) ([]NodeKeys, error) {
	identities := make([]NodeKeys, n)
	for i := 0; i < n; i++ {
		netKey, err := commons.GenNetworkKey()
		if err != nil {
			return nil, err
		}

		opPrivKey, err := keys.GeneratePrivateKey()
		if err != nil {
			return nil, err
		}

		identities[i] = NodeKeys{
			NetKey:      netKey,
			OperatorKey: opPrivKey,
		}
	}
	return identities, nil
}
