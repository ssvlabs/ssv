package ekm

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type KeyManager interface {
	spectypes.BeaconSigner
	SlashingProtector
	// AddShare decrypts and saves an encrypted share private key
	AddShare(encryptedSharePrivKey []byte) error
	// RemoveShare removes a share key
	RemoveShare(pubKey []byte) error
}

type ShareDecryptionError error
