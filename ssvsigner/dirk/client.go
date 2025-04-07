package dirk

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Client defines the interface for communication with a Dirk server.
// It encapsulates operations for listing, signing, and managing accounts.
type Client interface {
	// ListAccounts returns all accounts available in the Dirk server.
	ListAccounts(ctx context.Context) ([]Account, error)

	// Sign signs data using the specified account.
	// - ctx: Context for the operation
	// - pubKey: The public key identifying the validator share
	// - data: The signing root (hash) to be signed
	Sign(ctx context.Context, pubKey []byte, data []byte) ([]byte, error)

	// Close closes the connection to Dirk.
	Close() error
}

// Account represents a validator account in Dirk.
type Account struct {
	// PublicKey is the BLS public key of the validator.
	PublicKey phase0.BLSPubKey

	// Name is a human-readable identifier for the account.
	// Format is typically "wallet/account" such as "Wallet/validator-0".
	Name string
}

// Credentials holds authentication information for Dirk.
type Credentials struct {
	// ClientCert is the path to the client certificate file.
	ClientCert string

	// ClientKey is the path to the client private key file.
	ClientKey string

	// CACert is the path to the CA certificate file.
	CACert string

	// AllowInsecure allows insecure connections (not recommended for production).
	AllowInsecure bool

	// WalletName is the default wallet name to use with Dirk.
	// Usually "Wallet" or another name configured in Dirk.
	WalletName string

	// ConfigDir is the directory where Dirk configuration is stored.
	ConfigDir string
}
