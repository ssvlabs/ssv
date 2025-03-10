package ekm

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type KeyManager interface {
	spectypes.BeaconSigner
	SlashingProtector
	// AddShare decrypts and saves an encrypted share private key
	AddShare(encryptedSharePrivKey []byte, sharePubKey phase0.BLSPubKey) error
	// RemoveShare removes a share key
	RemoveShare(pubKey phase0.BLSPubKey) error
}

type ShareDecryptionError error
