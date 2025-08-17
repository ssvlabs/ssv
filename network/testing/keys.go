//go:build testutils

// This file contains helpers for tests only.
// It will not be compiled into production binaries.

package testing

import (
	"crypto/ecdsa"

	crand "crypto/rand"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/ssvsigner/keys"

	"github.com/libp2p/go-libp2p/core/crypto"

	"github.com/pkg/errors"
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
		netKey, err := GenNetworkKey()
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

// GenNetworkKey generates a new network key
func GenNetworkKey() (*ecdsa.PrivateKey, error) {
	privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	if err != nil {
		return nil, errors.WithMessage(err, "could not generate 256k1 key")
	}
	return commons.ECDSAPrivFromInterface(privInterfaceKey)
}
