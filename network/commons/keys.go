package commons

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	gcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/pkg/errors"
)

// GenNetworkKey generates a new network key
func GenNetworkKey() (*ecdsa.PrivateKey, error) {
	privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	if err != nil {
		return nil, errors.WithMessage(err, "could not generate 256k1 key")
	}
	privKey := (*ecdsa.PrivateKey)(privInterfaceKey.(*crypto.Secp256k1PrivateKey))
	privKey.Curve = gcrypto.S256()
	return privKey, nil
}
