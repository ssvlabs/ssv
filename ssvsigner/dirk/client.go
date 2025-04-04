package dirk

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
	// Configuration for ethdo command-line tool
	ethdoPath  string
	dirkWallet string
	configDir  string
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

	conn, err := grpc.DialContext(ctx, endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Dirk: %w", err)
	}

	ethdoPath, _ := exec.LookPath("ethdo")

	client := &GRPCClient{
		conn:         conn,
		listerClient: pb.NewListerClient(conn),
		signerClient: pb.NewSignerClient(conn),
		accountMgr:   pb.NewAccountManagerClient(conn),
		ethdoPath:    ethdoPath,
		dirkWallet:   c.WalletName,
		configDir:    c.ConfigDir,
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

// ImportAccount imports a keystore into Dirk
// Since Dirk doesn't have a direct gRPC API for importing keystores, we use two approaches:
// 1. If ethdo is available, we use it to import the keystore
// 2. Otherwise, we write the keystore to a temporary file in the Dirk configuration directory
//
// - ctx: Context for the operation
// - walletName: Name of the wallet to import to (e.g. "Wallet")
// - accountName: Name to give the account (e.g. "validator-0")
// - keystoreJSON: Raw keystore JSON data
// - keystorePass: Password to decrypt the keystore
// - walletPass: Optional passphrase for the wallet if required
func (g *GRPCClient) ImportAccount(ctx context.Context, walletName, accountName string,
	keystoreJSON, keystorePass, walletPass []byte) error {

	// Format full account name as wallet/account
	accountPath := fmt.Sprintf("%s/%s", walletName, accountName)

	// Try to use ethdo if available
	if g.ethdoPath != "" {
		return g.importWithEthdo(ctx, accountPath, keystoreJSON, keystorePass, walletPass)
	}

	// If no ethdo available, try to write to the config directory
	if g.configDir != "" {
		return g.importWithConfigDir(accountPath, keystoreJSON)
	}

	return fmt.Errorf("cannot import account: no ethdo command found and no config directory specified")
}

// importWithEthdo uses the ethdo command-line tool to import a keystore
func (g *GRPCClient) importWithEthdo(ctx context.Context, accountPath string, keystoreJSON, keystorePass, walletPass []byte) error {
	// Create a temporary file for the keystore
	tmpFile, err := os.CreateTemp("", "dirk-keystore-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write the keystore to the file
	if _, err := tmpFile.Write(keystoreJSON); err != nil {
		return fmt.Errorf("failed to write keystore to file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close keystore file: %w", err)
	}

	// Prepare the ethdo command
	// ethdo account import --account="wallet/account" --keystore=/path/to/keystore.json --passphrase="password"
	cmd := exec.CommandContext(ctx, g.ethdoPath,
		"account", "import",
		"--account="+accountPath,
		"--keystore="+tmpFile.Name(),
		"--passphrase="+string(keystorePass))

	// Add wallet passphrase if provided
	if walletPass != nil && len(walletPass) > 0 {
		cmd.Args = append(cmd.Args, "--wallet-passphrase="+string(walletPass))
	}

	// Run the command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to import account with ethdo: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// importWithConfigDir writes the keystore directly to the Dirk configuration directory
func (g *GRPCClient) importWithConfigDir(accountPath string, keystoreJSON []byte) error {
	// This is a simplified approach - the actual directory structure may vary based on Dirk's configuration
	keystoreDir := filepath.Join(g.configDir, "keystores")

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(keystoreDir, 0700); err != nil {
		return fmt.Errorf("failed to create keystore directory: %w", err)
	}

	// Create a file for the keystore
	// Naming is important - Dirk may expect a specific naming convention
	keystorePath := filepath.Join(keystoreDir, fmt.Sprintf("%s.json", accountPath))

	// Write the keystore to the file
	if err := os.WriteFile(keystorePath, keystoreJSON, 0600); err != nil {
		return fmt.Errorf("failed to write keystore to file: %w", err)
	}

	return nil
}

// DeleteAccount deletes an account from Dirk
// Similar to import, since Dirk doesn't have a direct gRPC API for deleting accounts,
// we use multiple approaches
// - ctx: Context for the operation
// - accountPath: Full path of the account (e.g. "Wallet/validator-0")
func (g *GRPCClient) DeleteAccount(ctx context.Context, accountPath string) error {
	// Try to use ethdo if available
	if g.ethdoPath != "" {
		return g.deleteWithEthdo(ctx, accountPath)
	}

	// If no ethdo available, try to delete from the config directory
	if g.configDir != "" {
		return g.deleteFromConfigDir(accountPath)
	}

	return fmt.Errorf("cannot delete account: no ethdo command found and no config directory specified")
}

// deleteWithEthdo uses the ethdo command-line tool to delete an account
func (g *GRPCClient) deleteWithEthdo(ctx context.Context, accountPath string) error {
	// TODO: delete account does not exist

	return nil
}

// deleteFromConfigDir removes the keystore file from the Dirk configuration directory
func (g *GRPCClient) deleteFromConfigDir(accountPath string) error {
	// TODO: fix all dirs based on the config dir
	// https://github.com/attestantio/dirk/blob/master/docs/configuration.md
	keystoreDir := filepath.Join(g.configDir, "keystores") // dummy path
	keystorePath := filepath.Join(keystoreDir, fmt.Sprintf("%s.json", accountPath))

	// Check if the file exists
	if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
		return fmt.Errorf("keystore file for account %s does not exist", accountPath)
	}

	// Delete the file
	if err := os.Remove(keystorePath); err != nil {
		return fmt.Errorf("failed to delete keystore file: %w", err)
	}

	return nil
}

// Sign signs data using the specified account
// - ctx: Context for the operation
// - pubKey: The public key that identifies the validator share
// - data: The signing root (hash) to be signed
// Returns the signature as a byte array
// TODO: declare error types
func (g *GRPCClient) Sign(ctx context.Context, pubKey []byte, data []byte) ([]byte, error) {
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
