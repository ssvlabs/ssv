package dirk

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/ssvsigner"
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

func (d *Signer) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, req web3signer.SignRequest) (web3signer.SignResponse, error) {
	if err := d.connect(ctx); err != nil {
		return web3signer.SignResponse{}, err
	}

	var blsSignature phase0.BLSSignature

	return web3signer.SignResponse{
		Signature: blsSignature,
	}, nil
}

// Ensure that Signer implements the RemoteSigner interface
var _ ssvsigner.RemoteSigner = (*Signer)(nil)
