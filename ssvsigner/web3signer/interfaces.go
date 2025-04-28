package web3signer

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// RemoteSigner defines the interface for interacting with a remote signing service.
type RemoteSigner interface {
	// ListKeys returns the list of validator public keys available on the remote signer.
	ListKeys(ctx context.Context) (ListKeysResponse, error)

	// ImportKeystore imports a keystore to the remote signer.
	ImportKeystore(ctx context.Context, req ImportKeystoreRequest) (ImportKeystoreResponse, error)

	// DeleteKeystore removes keystores from the remote signer.
	DeleteKeystore(ctx context.Context, req DeleteKeystoreRequest) (DeleteKeystoreResponse, error)

	// Sign requests a signature for the given public key and signing request.
	Sign(ctx context.Context, pubKey phase0.BLSPubKey, req SignRequest) (SignResponse, error)
}
