package types

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var blsPublicKeyCache *lru.Cache[string, bls.PublicKey]

func init() {
	var err error
	blsPublicKeyCache, err = lru.New[string, bls.PublicKey](128_000)
	if err != nil {
		panic(err)
	}
}

// DeserializeBLSPublicKey deserializes a bls.PublicKey from bytes,
// caching the result to avoid repeated deserialization.
func DeserializeBLSPublicKey(b []byte) (bls.PublicKey, error) {
	pkStr := string(b)
	if pk, ok := blsPublicKeyCache.Get(pkStr); ok {
		return pk, nil
	}

	// This copy is required to avoid the "cgo argument has Go pointer to Go pointer" panic.
	// TODO: (Alan) should we do this here or at the source? (wherever the bytes are coming from)
	pkCpy := make([]byte, len(b))
	copy(pkCpy, b)

	pk := bls.PublicKey{}
	if err := pk.Deserialize(pkCpy); err != nil {
		return bls.PublicKey{}, err
	}
	blsPublicKeyCache.Add(pkStr, pk)

	return pk, nil
}
