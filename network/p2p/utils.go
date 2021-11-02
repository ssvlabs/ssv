package p2p

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/prysmaticlabs/prysm/io/file"
	"path/filepath"
	"runtime"
)

// privKey determines a private key for p2p networking from the p2p service's
// configuration struct. If no key is found, it generates a new one
// TODO: private key is always generated instead of loaded from storage
func privKey() (*ecdsa.PrivateKey, error) {
	defaultKeyPath := defaultDataDir()

	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	rawbytes, err := priv.Raw()
	if err != nil {
		return nil, err
	}
	dst := make([]byte, hex.EncodedLen(len(rawbytes)))
	hex.Encode(dst, rawbytes)
	if err := file.WriteFile(defaultKeyPath, dst); err != nil {
		return nil, err
	}
	convertedKey := convertFromInterfacePrivKey(priv)
	return convertedKey, nil
}

func convertToInterfacePubkey(pubkey *ecdsa.PublicKey) crypto.PubKey {
	typeAssertedKey := crypto.PubKey((*crypto.Secp256k1PublicKey)(pubkey))
	return typeAssertedKey
}

// Adds a private key to the libp2p option if the option was provided.
// If the private key file is missing or cannot be read, or if the
// private key contents cannot be marshaled, an exception is thrown.
func privKeyOption(privkey *ecdsa.PrivateKey) libp2p.Option {
	return func(cfg *libp2p.Config) error {
		//log.Debug("ECDSA private key generated")
		return cfg.Apply(libp2p.Identity(convertToInterfacePrivkey(privkey)))
	}
}

func convertToInterfacePrivkey(privkey *ecdsa.PrivateKey) crypto.PrivKey {
	typeAssertedKey := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(privkey))
	return typeAssertedKey
}

func convertFromInterfacePrivKey(privkey crypto.PrivKey) *ecdsa.PrivateKey {
	typeAssertedKey := (*ecdsa.PrivateKey)(privkey.(*crypto.Secp256k1PrivateKey))
	typeAssertedKey.Curve = gcrypto.S256() // Temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1.
	return typeAssertedKey
}

func defaultDataDir() string {
	// Try to place the data folder in the user's home dir
	home := file.HomeDir()
	if home != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Eth2")
		} else if runtime.GOOS == "windows" {
			return filepath.Join(home, "AppData", "Local", "Eth2")
		} else {
			return filepath.Join(home, ".eth2")
		}
	}
	// As we cannot guess a stable location, return empty and handle later
	return ""
}

// pubKeyHash returns sha256 (hex) of the given public key
func pubKeyHash(pubkeyHex string) string {
	if len(pubkeyHex) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(pubkeyHex)))
}
