package types

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var (
	blsPublicKeyCache *lru.Cache[string, bls.PublicKey]
	blsSignatureCache *lru.Cache[string, bls.Sign]
)

func init() {
	var err error

	blsPublicKeyCache, err = lru.New[string, bls.PublicKey](128_000)
	if err != nil {
		panic(err)
	}

	blsSignatureCache, err = lru.New[string, bls.Sign](128_000)
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

	pk := bls.PublicKey{}
	if err := pk.Deserialize(b); err != nil {
		return bls.PublicKey{}, err
	}
	blsPublicKeyCache.Add(pkStr, pk)

	return pk, nil
}

// DeserializeBLSSignature deserializes a bls.Sign from bytes,
// caching the result to avoid repeated deserialization.
func DeserializeBLSSignature(b []byte) (bls.Sign, error) {
	sigStr := string(b)
	if sig, ok := blsSignatureCache.Get(sigStr); ok {
		return sig, nil
	}

	sig := bls.Sign{}
	if err := sig.Deserialize(b); err != nil {
		return bls.Sign{}, err
	}
	blsSignatureCache.Add(sigStr, sig)

	return sig, nil
}
