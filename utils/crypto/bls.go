package crypto

import (
	"github.com/cornelk/hashmap"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var blsPublicKeyCache = hashmap.New[string, bls.PublicKey]()

func DeserializeBLSPublicKey(b []byte) (bls.PublicKey, error) {
	pkStr := string(b)
	if pk, ok := blsPublicKeyCache.Get(pkStr); ok {
		return pk, nil
	}

	pk := bls.PublicKey{}
	if err := pk.Deserialize(b); err != nil {
		return bls.PublicKey{}, err
	}
	blsPublicKeyCache.Set(pkStr, pk)
	return pk, nil
}
