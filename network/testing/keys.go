package testing

import (
	"crypto/ecdsa"

	"github.com/herumi/bls-eth-go-binary/bls"

	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/utils/rsaencryption"
	"github.com/ssvlabs/ssv/utils/threshold"
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

func CreateKeysFromKeySet(ks *spectestingutils.TestKeySet) ([]NodeKeys, error) {
	identities := make([]NodeKeys, len(ks.OperatorKeys))
	for i, op := range ks.OperatorKeys {
		netKey, err := commons.GenNetworkKey()
		if err != nil {
			return nil, err
		}

		pk, err := keys.PrivateKeyFromBytes(rsaencryption.PrivateKeyToByte(op))
		if err != nil {
			return nil, err
		}
		identities[i-1] = NodeKeys{
			NetKey:      netKey,
			OperatorKey: pk,
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
