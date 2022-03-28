package testing

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	"crypto/rsa"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
)

// NodeKeys holds node's keys
type NodeKeys struct {
	NetKey      *ecdsa.PrivateKey
	OperatorKey *rsa.PrivateKey
}

var rsaKeySize = 2048

// CreateKeys creates <n> random node keys
func CreateKeys(n int) ([]NodeKeys, error) {
	identities := make([]NodeKeys, n)
	for i := 0; i < n; i++ {
		netKey, err := commons.GenNetworkKey()
		if err != nil {
			return nil, err
		}
		opKey, err := rsa.GenerateKey(crand.Reader, rsaKeySize)
		if err != nil {
			return nil, err
		}
		identities[i] = NodeKeys{
			NetKey:      netKey,
			OperatorKey: opKey,
		}
	}
	return identities, nil
}

// CreateShares creates n shares
func CreateShares(n int) []*bls.SecretKey {
	threshold.Init()

	var res []*bls.SecretKey
	for i := 0; i < n; i++ {
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()
		res = append(res, &sk)
	}
	return res
}
