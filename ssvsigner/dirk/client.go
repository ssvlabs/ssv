package dirk

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	pb "github.com/wealdtech/eth2-signer-api/pb/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	conn         *grpc.ClientConn
	listerClient pb.ListerClient
	signerClient pb.SignerClient
	accountMgr   pb.AccountManagerClient
}

func NewClient(ctx context.Context, endpoint string, c Credentials) (Client, error) {
	var dialOpts []grpc.DialOption

	if c.AllowInsecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig, err := createTLSConfig(c)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}

	// Connect to the Dirk server
	conn, err := grpc.DialContext(ctx, endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Dirk: %w", err)
	}

	client := &GRPCClient{
		conn:         conn,
		listerClient: pb.NewListerClient(conn),
		signerClient: pb.NewSignerClient(conn),
		accountMgr:   pb.NewAccountManagerClient(conn),
	}

	return client, nil
}

// createTLSConfig creates a TLS configuration from credentials
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

func (g *GRPCClient) ListAccounts() ([]Account, error) {
	// Create the request for listing accounts with wildcard path
	req := &pb.ListAccountsRequest{
		Paths: []string{"*"}, // Wildcard to list all accounts
	}

	// Call Dirk service to list accounts
	resp, err := g.listerClient.ListAccounts(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	if resp.GetState() != pb.ResponseState_SUCCEEDED {
		return nil, fmt.Errorf("failed to list accounts: state %v", resp.GetState())
	}

	// Convert the response to our Account type
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

// Sign signs data using a specified public key
// - pubKey: The public key that identifies the validator share
// - data: The signing root (hash) to be signed
// Returns the signature as a byte array
func (g *GRPCClient) Sign(pubKey []byte, data []byte) ([]byte, error) {
	req := &pb.SignRequest{
		Id: &pb.SignRequest_PublicKey{
			PublicKey: pubKey,
		},
		Data: data,
	}

	// Call the Dirk signer service
	ctx := context.Background()
	resp, err := g.signerClient.Sign(ctx, req)
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

func (g *GRPCClient) Close() error {
	if g.conn != nil {
		return g.conn.Close()
	}
	return nil
}
