package dirk

import (
	"context"
	"crypto/tls"
	"fmt"
	"google.golang.org/grpc/credentials"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	pb "github.com/wealdtech/eth2-signer-api/pb/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCClient implements Client interface for communication with Dirk via gRPC.
type GRPCClient struct {
	conn          *grpc.ClientConn
	listerClient  pb.ListerClient
	signerClient  pb.SignerClient
	accountMgr    pb.AccountManagerClient
	walletName    string
	configOptions ConfigOptions
}

// ConfigOptions contains optional configuration for the client.
type ConfigOptions struct {
	// EthdoPath is the path to the ethdo binary for offline operations.
	EthdoPath string

	// ConfigDir is the directory where Dirk stores its configuration.
	ConfigDir string
}

// NewGRPCClient creates a new Dirk gRPC client.
func NewGRPCClient(ctx context.Context, endpoint string, creds Credentials, opts ...ClientOption) (*GRPCClient, error) {
	var dialOpts []grpc.DialOption

	if creds.AllowInsecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig, err := createTLSConfig(creds)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}

	conn, err := grpc.DialContext(ctx, endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Dirk: %w", err)
	}

	client := &GRPCClient{
		conn:          conn,
		listerClient:  pb.NewListerClient(conn),
		signerClient:  pb.NewSignerClient(conn),
		accountMgr:    pb.NewAccountManagerClient(conn),
		walletName:    creds.WalletName,
		configOptions: ConfigOptions{},
	}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

// ClientOption defines a function to configure a GRPCClient.
type ClientOption func(*GRPCClient)

// WithEthdoPath sets the path to the ethdo binary.
func WithEthdoPath(path string) ClientOption {
	return func(c *GRPCClient) {
		c.configOptions.EthdoPath = path
	}
}

// WithConfigDir sets the config directory.
func WithConfigDir(dir string) ClientOption {
	return func(c *GRPCClient) {
		c.configOptions.ConfigDir = dir
	}
}

// createTLSConfig creates a TLS configuration from credentials.
func createTLSConfig(credentials Credentials) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(credentials.ClientCert, credentials.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Create TLS config
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	return tlsConfig, nil
}

// ListAccounts returns all accounts available in the Dirk server.
func (c *GRPCClient) ListAccounts(ctx context.Context) ([]Account, error) {
	// Create the request for listing accounts with wildcard path
	req := &pb.ListAccountsRequest{
		Paths: []string{"*"}, // Wildcard to list all accounts
	}

	// Call Dirk service to list accounts
	resp, err := c.listerClient.ListAccounts(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	if resp.GetState() != pb.ResponseState_SUCCEEDED {
		return nil, fmt.Errorf("failed to list accounts: state %v", resp.GetState())
	}

	// Convert the response to our Account type
	accounts := make([]Account, 0, len(resp.GetAccounts()))
	for _, account := range resp.GetAccounts() {
		// Convert the public key to our format
		var pubKey phase0.BLSPubKey
		if len(account.GetPublicKey()) != len(pubKey) {
			return nil, fmt.Errorf("invalid public key length: got %d, want %d", len(account.GetPublicKey()), len(pubKey))
		}
		copy(pubKey[:], account.GetPublicKey())

		accounts = append(accounts, Account{
			PublicKey: pubKey,
			Name:      account.GetName(),
		})
	}

	return accounts, nil
}

// Sign signs data using the specified account.
func (c *GRPCClient) Sign(ctx context.Context, pubKey []byte, data []byte) ([]byte, error) {
	// Create a sign request with the public key as identifier
	req := &pb.SignRequest{
		// Use public key as identifier
		Id: &pb.SignRequest_PublicKey{
			PublicKey: pubKey,
		},
		// The data to sign (signing root)
		Data: data,
	}

	// Call the Dirk signer service
	resp, err := c.signerClient.Sign(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dirk signing failed: %w", err)
	}

	// Check the response state
	if resp.GetState() != pb.ResponseState_SUCCEEDED {
		return nil, fmt.Errorf("dirk signing failed with state: %v", resp.GetState())
	}

	// Return the signature bytes
	return resp.GetSignature(), nil
}

// Close closes the gRPC connection.
func (c *GRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
