package types

import "github.com/herumi/bls-eth-go-binary/bls"

type PubKeys []*bls.PublicKey

func (keys PubKeys) Aggregate() bls.PublicKey {
	ret := bls.PublicKey{}
	for _, k := range keys {
		ret.Add(k)
	}
	return ret
}
