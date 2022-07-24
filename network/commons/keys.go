package commons

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"math/big"

	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/pkg/errors"
)

func ConvertFromInterfacePrivKey(privkey crypto.PrivKey) (*ecdsa.PrivateKey, error) {
	secpKey := (privkey.(*crypto.Secp256k1PrivateKey))
	rawKey, err := secpKey.Raw()
	if err != nil {
		return nil, err
	}
	privKey := new(ecdsa.PrivateKey)
	k := new(big.Int).SetBytes(rawKey)
	privKey.D = k
	privKey.Curve = gcrypto.S256() // Temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1.
	privKey.X, privKey.Y = gcrypto.S256().ScalarBaseMult(rawKey)
	return privKey, nil
}

func ConvertToInterfacePrivkey(privkey *ecdsa.PrivateKey) (crypto.PrivKey, error) {
	return crypto.UnmarshalSecp256k1PrivateKey(privkey.D.Bytes())
}

// GenNetworkKey generates a new network key
func GenNetworkKey() (*ecdsa.PrivateKey, error) {
	privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	if err != nil {
		return nil, errors.WithMessage(err, "could not generate 256k1 key")
	}
	return ConvertFromInterfacePrivKey(privInterfaceKey)
}
