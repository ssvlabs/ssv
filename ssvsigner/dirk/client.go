package dirk

import (
	"context"
	"crypto/tls"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	conn *grpc.ClientConn
	// TODO: add generated protobuf
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
		conn: conn,
		// Initialize service clients
		// listerClient:   pb.NewListerClient(conn),
		// signerClient:   pb.NewSignerClient(conn),
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

func (G GRPCClient) ListAccounts() ([]Account, error) {
	//TODO implement me
	panic("implement me")
}

func (G GRPCClient) Sign(pubKey []byte, data []byte, domain []byte) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (G GRPCClient) Close() error {
	//TODO implement me
	panic("implement me")
}
