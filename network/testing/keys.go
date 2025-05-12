package testing

import (
	"context"
	"crypto/ecdsa"

	"github.com/herumi/bls-eth-go-binary/bls"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
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

		pk, err := keys.PrivateKeyFromBytes(rsaencryption.PrivateKeyToPEM(op))
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

// NewLocalTestnet creates a new local network from test keys set
func NewLocalTestnetFromKeySet(ctx context.Context, factory NetworkFactory, ks *spectestingutils.TestKeySet) ([]network.P2PNetwork, []NodeKeys, error) {
	nodes := make([]network.P2PNetwork, len(ks.OperatorKeys))
	keys, err := CreateKeysFromKeySet(ks)
	if err != nil {
		return nil, nil, err
	}

	i := uint64(0)
	for _, k := range keys {
		nodes[i] = factory(ctx, i, k)
	}

	return nodes, keys, nil
}
