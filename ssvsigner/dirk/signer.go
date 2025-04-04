package dirk

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// Signer is the implementation of web3signer.RemoteSigner for Dirk
// TODO: update global interfaces, fix all references to web3signer.RemoteSigner
type Signer struct {
	endpoint    string      // Endpoint of the Dirk server (host:port)
	credentials Credentials // Credentials for connecting to the Dirk server
	client      Client      // Client for communicating with the Dirk server
}

// New creates a new Dirk signer
func New(endpoint string, credentials Credentials) *Signer {
	return &Signer{
		endpoint:    endpoint,
		credentials: credentials,
	}
}

// Connect establishes a connection to the Dirk server
func (s *Signer) connect(ctx context.Context) error {
	if s.client != nil {
		return nil
	}

	client, err := NewClient(ctx, s.endpoint, s.credentials)
	if err != nil {
		return fmt.Errorf("failed to connect to Dirk: %w", err)
	}

	s.client = client
	return nil
}

// ListKeys lists all available validator keys
func (s *Signer) ListKeys(ctx context.Context) (web3signer.ListKeysResponse, error) {
	if err := s.connect(ctx); err != nil {
		return nil, err
	}

	accounts, err := s.client.ListAccounts()
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	response := make(web3signer.ListKeysResponse, 0, len(accounts))
	for _, account := range accounts {
		response = append(response, account.PublicKey)
	}

	return response, nil
}

// Sign signs a message using Dirk's gRPC API
func (s *Signer) Sign(ctx context.Context, pubKey phase0.BLSPubKey, req web3signer.SignRequest) (web3signer.SignResponse, error) {
	if err := s.connect(ctx); err != nil {
		return web3signer.SignResponse{}, err
	}

	pubKeyBytes := pubKey[:]
	var signingRootBytes []byte

	if req.SigningRoot != [32]byte{} {
		signingRootBytes = req.SigningRoot[:]
	} else {
		return web3signer.SignResponse{}, fmt.Errorf("signing without explicit signing root is not supported")
	}

	signature, err := s.client.Sign(ctx, pubKeyBytes, signingRootBytes)
	if err != nil {
		return web3signer.SignResponse{}, fmt.Errorf("failed to sign data: %w", err)
	}

	var blsSignature phase0.BLSSignature
	if len(signature) != len(blsSignature) {
		return web3signer.SignResponse{}, fmt.Errorf("invalid signature length: got %d, want %d", len(signature), len(blsSignature))
	}
	copy(blsSignature[:], signature)

	return web3signer.SignResponse{
		Signature: blsSignature,
	}, nil
}

// SignDirect signs data directly without using Web3Signer format
func (s *Signer) SignDirect(ctx context.Context, pubKeyBytes, signingRoot []byte) ([]byte, error) {
	if err := s.connect(ctx); err != nil {
		return nil, err
	}

	return s.client.Sign(ctx, pubKeyBytes, signingRoot)
}

// Close closes the connection to the Dirk server
func (s *Signer) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// Helper method to get the ethdo path (find in PATH if not specified)
func (s *Signer) getEthdoPath() string {
	// Use existing path from client if available
	if gc, ok := s.client.(*GRPCClient); ok && gc.ethdoPath != "" {
		return gc.ethdoPath
	}

	// Otherwise look in PATH
	path, _ := exec.LookPath("ethdo")
	return path
}
