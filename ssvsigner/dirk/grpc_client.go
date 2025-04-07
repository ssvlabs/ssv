package dirk

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"google.golang.org/grpc/credentials"

	pb "github.com/wealdtech/eth2-signer-api/pb/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCClient implements Client interface for communication with Dirk via gRPC.
type GRPCClient struct {
	conn         *grpc.ClientConn
	listerClient pb.ListerClient
	signerClient pb.SignerClient
	accountMgr   pb.AccountManagerClient
	walletName   string
}

// NewGRPCClient creates a new Dirk gRPC client.
// ClientOptions are no longer used as ethdo config is externalized.
func NewGRPCClient(ctx context.Context, endpoint string, creds Credentials) (*GRPCClient, error) {
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
		conn:         conn,
		listerClient: pb.NewListerClient(conn),
		signerClient: pb.NewSignerClient(conn),
		accountMgr:   pb.NewAccountManagerClient(conn),
		walletName:   creds.WalletName,
	}

	return client, nil
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

	// Call the Dirk Signer service
	resp, err := c.signerClient.Sign(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("gRPC call to Dirk Sign failed: %w", err)
	}

	// Check the response state from Dirk
	if resp.GetState() != pb.ResponseState_SUCCEEDED {
		return nil, fmt.Errorf("dirk Sign returned non-success state: %v", resp.GetState())
	}

	// Return the signature bytes from the successful response
	return resp.GetSignature(), nil
}

// ListAccounts returns all accounts available in the Dirk server.
func (c *GRPCClient) ListAccounts(ctx context.Context) ([]Account, error) {
	req := &pb.ListAccountsRequest{
		Paths: []string{"*"}, // Wildcard to list all accounts
	}

	// Call Dirk Lister service
	resp, err := c.listerClient.ListAccounts(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("gRPC call to Dirk ListAccounts failed: %w", err)
	}

	// Check response state from Dirk
	if resp.GetState() != pb.ResponseState_SUCCEEDED {
		return nil, fmt.Errorf("dirk ListAccounts returned non-success state: %v", resp.GetState())
	}

	// Convert the gRPC response accounts to our internal Account type
	accounts := make([]Account, 0, len(resp.GetAccounts()))
	for _, account := range resp.GetAccounts() {
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

// Close closes the gRPC connection.
func (c *GRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
