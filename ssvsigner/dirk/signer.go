package dirk

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"

	// "os/exec" // No longer needed after removing findEthdoPath

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// Signer implements web3signer.RemoteSigner for the Dirk signing service.
// It handles communication with a Dirk server and adapts its API to the expected
// web3signer interfaces for listing keys, signing, and key management (import only) operations.
// It requires a path to a functional ethdo executable for import operations.
type Signer struct {
	endpoint    string      // Endpoint of the Dirk server (host:port)
	credentials Credentials // Credentials for connecting to the Dirk server
	client      Client      // Lazily initialized gRPC client for communication
	ethdoPath   string      // Path to the ethdo executable, required for imports
}

// New creates a new Dirk signer.
// It requires the Dirk server endpoint, credentials, and the path to the ethdo executable.
func New(endpoint string, credentials Credentials, ethdoPath string) (*Signer, error) {
	if ethdoPath == "" {
		// Attempt to find ethdo in PATH as a fallback? Or enforce it?
		// Let's enforce it for clarity.
		return nil, fmt.Errorf("ethdo path must be provided for Dirk signer")
	}
	// check if ethdoPath is executable
	if _, err := exec.LookPath(ethdoPath); err != nil {
		return nil, fmt.Errorf("ethdo executable not found at path %s: %w", ethdoPath, err)
	}

	return &Signer{
		endpoint:    endpoint,
		credentials: credentials,
		ethdoPath:   ethdoPath,
	}, nil
}

// connect establishes a connection to the Dirk server if not already connected.
func (s *Signer) connect(ctx context.Context) error {
	if s.client != nil {
		return nil
	}

	client, err := NewGRPCClient(
		ctx,
		s.endpoint,
		s.credentials,
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
// This method uses the KeystoreManager which relies on the ethdo CLI tool.
func (s *Signer) ImportKeystore(ctx context.Context, req web3signer.ImportKeystoreRequest) (web3signer.ImportKeystoreResponse, error) {
	if err := s.connect(ctx); err != nil {
		return web3signer.ImportKeystoreResponse{}, err
	}

	// Use the ethdo path configured for the Signer.
	// The New function already validated that s.ethdoPath is not empty.
	ethdoPath := s.ethdoPath

	// Create EthdoManager
	manager, err := NewEthdoManager(ethdoPath, s.credentials.WalletName)
	if err != nil {
		return web3signer.ImportKeystoreResponse{}, fmt.Errorf("failed to initialize keystore manager: %w", err)
	}

	// Configure additional ethdo options if TLS is being used or other options are set
	// Note: s.credentials.EthdoBaseDir might be relevant for ethdo's --base-dir if needed.
	// Construct EthdoOptions based on available credentials.
	opts := EthdoOptions{
		BaseDir:      s.credentials.EthdoBaseDir,
		ServerCACert: s.credentials.CACert,
		ClientCert:   s.credentials.ClientCert,
		ClientKey:    s.credentials.ClientKey,
		Remote:       s.endpoint, // Use the signer's endpoint as the potential remote target for ethdo
	}
	// Only call WithEthdoOptions if any options are actually set.
	if opts != (EthdoOptions{}) { // Check if the struct is non-zero
		manager.WithEthdoOptions(opts)
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
// WARNING: Deleting individual accounts/keystores is NOT supported by ethdo or the Dirk API.
// This function will always return an error indicating the operation is unsupported.
// Manual deletion via filesystem is unreliable and not recommended.
func (s *Signer) DeleteKeystore(ctx context.Context, req web3signer.DeleteKeystoreRequest) (web3signer.DeleteKeystoreResponse, error) {
	// Deletion is not supported. Return an error for all requested keys.
	responseData := make([]web3signer.KeyManagerResponseData, len(req.Pubkeys))
	errMsg := "keystore deletion is not supported by the Dirk signer implementation"

	for i, pubkey := range req.Pubkeys {
		responseData[i] = web3signer.KeyManagerResponseData{
			Status:  web3signer.StatusError,
			Message: fmt.Sprintf("failed to delete account %s: %s", pubkey.String(), errMsg),
		}
	}

	// Return a general error as well
	return web3signer.DeleteKeystoreResponse{
		Data: responseData,
	}, fmt.Errorf(errMsg)
}

// Close closes the connection to the Dirk server.
func (s *Signer) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// decodeKeystoreJSON decodes a base64-encoded keystore JSON string.
func decodeKeystoreJSON(encodedKeystore string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encodedKeystore)
}

// processKeystore handles a single keystore import using the EthdoManager.
func processKeystore(
	ctx context.Context,
	manager *EthdoManager, // Use EthdoManager type
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

	// Import the keystore using EthdoManager
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
