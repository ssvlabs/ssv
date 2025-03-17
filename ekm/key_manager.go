package ekm

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/signing"
)

type KeyManager interface {
	signing.BeaconSigner
	SlashingProtector
	// AddShare decrypts and saves an encrypted share private key
	AddShare(ctx context.Context, encryptedSharePrivKey []byte, sharePubKey phase0.BLSPubKey) error
	// RemoveShare removes a share key
	RemoveShare(ctx context.Context, pubKey phase0.BLSPubKey) error
}

type ShareDecryptionError error
