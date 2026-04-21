package commons

import (
	"crypto/ecdsa"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p/core/crypto"
)

// ECDSAPrivFromInterface converts crypto.PrivKey back to ecdsa.PrivateKey
func ECDSAPrivFromInterface(privkey crypto.PrivKey) (*ecdsa.PrivateKey, error) {
	if privkey == nil {
		return nil, errors.New("private key is nil")
	}

	secpKey, ok := privkey.(*crypto.Secp256k1PrivateKey)
	if !ok || secpKey == nil {
		return nil, fmt.Errorf("unsupported key type: expected Secp256k1 private key, got %T", privkey)
	}

	rawKey, err := secpKey.Raw()
	if err != nil {
		return nil, fmt.Errorf("could not convert ecdsa.PrivateKey: %w", err)
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
func ECDSAPubFromInterface(pubKey crypto.PubKey) (*ecdsa.PublicKey, error) {
	if pubKey == nil {
		return nil, errors.New("public key is nil")
	}

	secpKey, ok := pubKey.(*crypto.Secp256k1PublicKey)
	if !ok || secpKey == nil {
		return nil, fmt.Errorf("unsupported key type: expected Secp256k1 public key, got %T", pubKey)
	}

	pk := btcec.PublicKey(*secpKey)
	return pk.ToECDSA(), nil
}

// ECDSAPubToInterface converts ecdsa.PublicKey to crypto.PubKey
func ECDSAPubToInterface(pubkey *ecdsa.PublicKey) (crypto.PubKey, error) {
	xVal, yVal := new(btcec.FieldVal), new(btcec.FieldVal)
	if xVal.SetByteSlice(pubkey.X.Bytes()) {
		return nil, fmt.Errorf("X value overflows")
	}
	if yVal.SetByteSlice(pubkey.Y.Bytes()) {
		return nil, fmt.Errorf("Y value overflows")
	}

	newKey := crypto.PubKey((*crypto.Secp256k1PublicKey)(btcec.NewPublicKey(xVal, yVal)))
	// Zero out temporary values.
	xVal.Zero()
	yVal.Zero()
	return newKey, nil
}
