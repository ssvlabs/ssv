package dirk

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// New creates a new Dirk signer
func New(endpoint string, credentials Credentials) *Signer {
	return &Signer{
		endpoint:    endpoint,
		credentials: credentials,
	}
}

func (d *Signer) connect(ctx context.Context) error {
	if d.client != nil {
		return nil
	}

	client, err := NewClient(ctx, d.endpoint, d.credentials)
	if err != nil {
		return err
	}

	d.client = client
	return nil
}

func (d *Signer) ListKeys(ctx context.Context) (web3signer.ListKeysResponse, error) {
	if err := d.connect(ctx); err != nil {
		return nil, err
	}

	accounts, err := d.client.ListAccounts()
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	response := make(web3signer.ListKeysResponse, 0, len(accounts))
	for _, account := range accounts {
		response = append(response, account.PublicKey)
	}

	return response, nil
}

func (d *Signer) ImportKeystore(ctx context.Context, req web3signer.ImportKeystoreRequest) (web3signer.ImportKeystoreResponse, error) {
	return web3signer.ImportKeystoreResponse{
		Data: []web3signer.KeyManagerResponseData{
			{
				Status:  web3signer.StatusImported,
				Message: "not implemented",
			},
		},
	}, nil
}

func (d *Signer) DeleteKeystore(ctx context.Context, req web3signer.DeleteKeystoreRequest) (web3signer.DeleteKeystoreResponse, error) {
	return web3signer.DeleteKeystoreResponse{
		Message: ErrKeyDeletionNotSupported.Error(),
		Data: []web3signer.KeyManagerResponseData{
			{
				Status:  web3signer.StatusError,
				Message: ErrKeyDeletionNotSupported.Error(),
			},
		},
	}, ErrKeyDeletionNotSupported
}

// Sign signs a message using Dirk's gRPC API
// The function takes a public key and signing root (hash to sign)
// It returns a BLS signature or an error
func (d *Signer) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, signingRoot phase0.Root) (phase0.BLSSignature, error) {
	if err := d.connect(ctx); err != nil {
		return phase0.BLSSignature{}, fmt.Errorf("failed to connect to Dirk: %w", err)
	}

	// Convert BLS public key to byte array
	pubKeyBytes := sharePubKey[:]

	// Convert signing root to byte array
	signRootBytes := signingRoot[:]

	signature, err := d.client.Sign(ctx, pubKeyBytes, signRootBytes)
	if err != nil {
		return phase0.BLSSignature{}, fmt.Errorf("failed to sign data: %w", err)
	}

	// Convert the signature to the expected BLSSignature format
	var blsSignature phase0.BLSSignature
	if len(signature) != len(blsSignature) {
		return phase0.BLSSignature{}, fmt.Errorf("invalid signature length: got %d, want %d", len(signature), len(blsSignature))
	}
	copy(blsSignature[:], signature)

	return blsSignature, nil
}

// Ensure that Signer implements the RemoteSigner interface
//var _ ssvsigner.RemoteSigner = (*Signer)(nil)
