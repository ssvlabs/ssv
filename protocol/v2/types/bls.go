package types

import (
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"
)

var blsPublicKeyCache *lru.Cache[string, *BLSTPublicKey]

func init() {
	var err error
	blsPublicKeyCache, err = lru.New[string, *BLSTPublicKey](128_000)
	if err != nil {
		panic(err)
	}
}

// DeserializeBLSPublicKey deserializes a bls.PublicKey from bytes,
// caching the result to avoid repeated deserialization.
func DeserializeBLSPublicKey(b []byte) (*BLSTPublicKey, error) {
	pkStr := string(b)
	if pk, ok := blsPublicKeyCache.Get(pkStr); ok {
		return pk, nil
	}

	pk := new(BLSTPublicKey).Uncompress(b)
	if pk == nil {
		return nil, fmt.Errorf("failed to deserialize public key")
	}

	blsPublicKeyCache.Add(pkStr, pk)

	return pk, nil
}
