package dirk

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"os/exec"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// Signer implements web3signer.RemoteSigner for the Dirk signing service.
// It handles communication with a Dirk server and adapts its API to the expected
// web3signer interfaces for listing keys, signing, and key management operations.
type Signer struct {
	endpoint    string      // Endpoint of the Dirk server (host:port)
	credentials Credentials // Credentials for connecting to the Dirk server
	client      Client      // Client for communicating with the Dirk server
}

// New creates a new Dirk signer.
func New(endpoint string, credentials Credentials) *Signer {
	return &Signer{
		endpoint:    endpoint,
		credentials: credentials,
	}
}

// connect establishes a connection to the Dirk server if not already connected.
func (s *Signer) connect(ctx context.Context) error {
	if s.client != nil {
		return nil
	}

	// Find ethdo executable path for offline operations
	ethdoPath, _ := exec.LookPath("ethdo")

	// Create client with options
	client, err := NewGRPCClient(
		ctx,
		s.endpoint,
		s.credentials,
		WithEthdoPath(ethdoPath),
		WithConfigDir(s.credentials.ConfigDir),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to Dirk: %w", err)
	}

	s.client = client
	return nil
}

// Sign implements web3signer.RemoteSigner.Sign.
// It uses the Dirk gRPC API to sign data with the specified validator key.
func (s *Signer) Sign(ctx context.Context, pubKey phase0.BLSPubKey, req web3signer.SignRequest) (web3signer.SignResponse, error) {
	if err := s.connect(ctx); err != nil {
		return web3signer.SignResponse{}, err
	}

	// Dirk only supports signing with explicit signing root
	var signingRootBytes []byte
	if req.SigningRoot != [32]byte{} {
		signingRootBytes = req.SigningRoot[:]
	} else {
		return web3signer.SignResponse{}, fmt.Errorf("signing without explicit signing root is not supported")
	}

	// Call Dirk to sign the data
	signature, err := s.client.Sign(ctx, pubKey[:], signingRootBytes)
	if err != nil {
		return web3signer.SignResponse{}, fmt.Errorf("failed to sign data: %w", err)
	}

	// Verify signature length and copy to BLSSignature
	var blsSignature phase0.BLSSignature
	if len(signature) != len(blsSignature) {
		return web3signer.SignResponse{}, fmt.Errorf("invalid signature length: got %d, want %d", len(signature), len(blsSignature))
	}
	copy(blsSignature[:], signature)

	return web3signer.SignResponse{
		Signature: blsSignature,
	}, nil
}

// ListKeys implements web3signer.RemoteSigner.ListKeys.
// It fetches and returns all validator public keys registered with the Dirk server.
func (s *Signer) ListKeys(ctx context.Context) (web3signer.ListKeysResponse, error) {
	if err := s.connect(ctx); err != nil {
		return nil, err
	}

	accounts, err := s.client.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	response := make(web3signer.ListKeysResponse, 0, len(accounts))
	for _, account := range accounts {
		response = append(response, account.PublicKey)
	}

	return response, nil
}

// ImportKeystore implements web3signer.RemoteSigner.ImportKeystore.
// Since Dirk doesn't support direct keystore imports via API, this method uses
// the KeystoreManager which employs ethdo CLI or direct file placement.
func (s *Signer) ImportKeystore(ctx context.Context, req web3signer.ImportKeystoreRequest) (web3signer.ImportKeystoreResponse, error) {
	if err := s.connect(ctx); err != nil {
		return web3signer.ImportKeystoreResponse{}, err
	}

	manager := NewKeystoreManager(
		s.client,
		findEthdoPath(s.client),
		s.credentials.ConfigDir,
		s.credentials.WalletName,
	)

	// Configure additional ethdo options if TLS is being used
	if s.credentials.CACert != "" || s.credentials.ClientCert != "" {
		manager.WithEthdoOptions(
			s.credentials.ConfigDir,
			s.credentials.CACert,
			s.credentials.ClientCert,
			s.credentials.ClientKey,
			s.endpoint,
		)
	}

	// Process each keystore in the request
	responseData := make([]web3signer.KeyManagerResponseData, 0, len(req.Keystores))
	for i, encodedKeystore := range req.Keystores {
		// Get password for this keystore
		var password string
		if i < len(req.Passwords) {
			password = req.Passwords[i]
		} else {
			responseData = append(responseData, web3signer.KeyManagerResponseData{
				Status:  web3signer.StatusError,
				Message: fmt.Sprintf("no password provided for keystore %d", i),
			})
			continue
		}

		// Process this keystore
		result, err := processKeystore(ctx, manager, encodedKeystore, password, s.credentials.WalletName)
		responseData = append(responseData, result)
		if err != nil {
			continue
		}
	}

	return web3signer.ImportKeystoreResponse{
		Data: responseData,
	}, nil
}

// DeleteKeystore implements web3signer.RemoteSigner.DeleteKeystore.
// Since Dirk doesn't support runtime key deletion, this implementation will
// delete the keystore files but requires a Dirk restart to take effect.
func (s *Signer) DeleteKeystore(ctx context.Context, req web3signer.DeleteKeystoreRequest) (web3signer.DeleteKeystoreResponse, error) {
	if err := s.connect(ctx); err != nil {
		return web3signer.DeleteKeystoreResponse{}, err
	}

	responseData := make([]web3signer.KeyManagerResponseData, 0, len(req.Pubkeys))

	for _, pubkey := range req.Pubkeys {
		err := DeleteByPublicKey(ctx, s.client, s.credentials.ConfigDir, pubkey)
		if err != nil {
			responseData = append(responseData, web3signer.KeyManagerResponseData{
				Status:  web3signer.StatusError,
				Message: fmt.Sprintf("failed to delete account: %v", err),
			})
			continue
		}

		responseData = append(responseData, web3signer.KeyManagerResponseData{
			Status:  web3signer.StatusDeleted,
			Message: fmt.Sprintf("deleted account with public key %s (restart Dirk to use)", pubkey.String()),
		})
	}

	return web3signer.DeleteKeystoreResponse{
		Data: responseData,
	}, nil
}

// SignDirect signs data directly without using Web3Signer format.
// This is a convenience method for direct signing needs.
func (s *Signer) SignDirect(ctx context.Context, pubKeyBytes, signingRoot []byte) ([]byte, error) {
	if err := s.connect(ctx); err != nil {
		return nil, err
	}

	return s.client.Sign(ctx, pubKeyBytes, signingRoot)
}

// Close closes the connection to the Dirk server.
func (s *Signer) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// Helper functions

// decodeKeystoreJSON decodes a base64-encoded keystore JSON string.
func decodeKeystoreJSON(encodedKeystore string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encodedKeystore)
}

// findEthdoPath tries to find the ethdo executable path, first checking
// if the client already has it configured, then looking in PATH.
func findEthdoPath(client Client) string {
	if gc, ok := client.(*GRPCClient); ok && gc.configOptions.EthdoPath != "" {
		return gc.configOptions.EthdoPath
	}

	path, _ := exec.LookPath("ethdo")
	return path
}

// processKeystore handles a single keystore import.
func processKeystore(
	ctx context.Context,
	manager *KeystoreManager,
	encodedKeystore string,
	password string,
	walletName string,
) (web3signer.KeyManagerResponseData, error) {
	// Decode base64 keystore
	keystoreJSON, err := decodeKeystoreJSON(encodedKeystore)
	if err != nil {
		return web3signer.KeyManagerResponseData{
			Status:  web3signer.StatusError,
			Message: fmt.Sprintf("failed to decode keystore: %v", err),
		}, err
	}

	// Import the keystore
	err = manager.ImportKeystoreJSON(
		ctx,
		keystoreJSON,
		password,
		walletName,
		"", // Let KeystoreManager generate the account name
	)

	if err != nil {
		return web3signer.KeyManagerResponseData{
			Status:  web3signer.StatusError,
			Message: fmt.Sprintf("failed to import keystore: %v", err),
		}, err
	}

	return web3signer.KeyManagerResponseData{
		Status:  web3signer.StatusImported,
		Message: "Imported keystore. Note: Dirk requires restart to use new keys.",
	}, nil
}
