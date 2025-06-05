package commons

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"

	"github.com/btcsuite/btcd/btcec/v2"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/pkg/errors"
)

// ECDSAPrivFromInterface converts crypto.PrivKey back to ecdsa.PrivateKey
func ECDSAPrivFromInterface(privkey crypto.PrivKey) (*ecdsa.PrivateKey, error) {
	secpKey := privkey.(*crypto.Secp256k1PrivateKey)
	rawKey, err := secpKey.Raw()
	if err != nil {
		return nil, errors.Wrap(err, "could not convert ecdsa.PrivateKey")
	}

	privKey, _ := btcec.PrivKeyFromBytes(rawKey)
	ecdsaKey := privKey.ToECDSA()
	ecdsaKey.Curve = gcrypto.S256() // temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1
	return ecdsaKey, nil
}

// ECDSAPrivToInterface converts ecdsa.PrivateKey to crypto.PrivKey
func ECDSAPrivToInterface(privkey *ecdsa.PrivateKey) (crypto.PrivKey, error) {
	privBytes := privkey.D.Bytes()
	// In the event the number of bytes outputted by the big-int are less than 32,
	// we append bytes to the start of the sequence for the missing most significant
	// bytes.
	if len(privBytes) < 32 {
		privBytes = append(make([]byte, 32-len(privBytes)), privBytes...)
	}
	return crypto.UnmarshalSecp256k1PrivateKey(privBytes)
}

// ECDSAPubFromInterface converts crypto.PubKey to ecdsa.PublicKey
func ECDSAPubFromInterface(pubKey crypto.PubKey) *ecdsa.PublicKey {
	pk := btcec.PublicKey(*(pubKey.(*crypto.Secp256k1PublicKey)))
	return pk.ToECDSA()
}

// ECDSAPubToInterface converts ecdsa.PublicKey to crypto.PubKey
func ECDSAPubToInterface(pubkey *ecdsa.PublicKey) (crypto.PubKey, error) {
	xVal, yVal := new(btcec.FieldVal), new(btcec.FieldVal)
	if xVal.SetByteSlice(pubkey.X.Bytes()) {
		return nil, errors.Errorf("X value overflows")
	}
	if yVal.SetByteSlice(pubkey.Y.Bytes()) {
		return nil, errors.Errorf("Y value overflows")
	}

	newKey := crypto.PubKey((*crypto.Secp256k1PublicKey)(btcec.NewPublicKey(xVal, yVal)))
	// Zero out temporary values.
	xVal.Zero()
	yVal.Zero()
	return newKey, nil
}

// RSAPrivToInterface converts rsa.PrivateKey to crypto.PrivKey
func RSAPrivToInterface(privkey *rsa.PrivateKey) (crypto.PrivKey, error) {
	rsaPrivDER := x509.MarshalPKCS1PrivateKey(privkey)
	return crypto.UnmarshalRsaPrivateKey(rsaPrivDER)
}

// GenNetworkKey generates a new network key
func GenNetworkKey() (*ecdsa.PrivateKey, error) {
	privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	if err != nil {
		return nil, errors.WithMessage(err, "could not generate 256k1 key")
	}
	return ECDSAPrivFromInterface(privInterfaceKey)
}
