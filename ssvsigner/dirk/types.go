package dirk

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Client defines the interface for communication with a Dirk server
type Client interface {
	// ListAccounts returns all accounts available in the Dirk server
	ListAccounts() ([]Account, error)

	// ImportAccount imports a keystore to Dirk
	// walletName: Name of the wallet to import to (e.g. "Wallet")
	// accountName: Name to give the account (e.g. "validator-0")
	// keystoreJSON: Raw keystore JSON data
	// keystorePass: Password to decrypt the keystore
	// walletPass: Optional passphrase for the wallet if required
	ImportAccount(ctx context.Context, walletName, accountName string,
		keystoreJSON, keystorePass, walletPass []byte) error

	// DeleteAccount deletes an account from Dirk
	// accountPath: Full path of the account (e.g. "Wallet/validator-0")
	DeleteAccount(ctx context.Context, accountPath string) error

	// Sign signs data using the specified account
	// ctx: Context for the operation
	// pubKey: The public key identifying the validator share
	// data: The signing root (hash) to be signed
	Sign(ctx context.Context, pubKey []byte, data []byte) ([]byte, error)

	// Close closes the connection to Dirk
	Close() error
}

// Credentials holds authentication information for Dirk
type Credentials struct {
	// ClientCert is the path to the client certificate file
	ClientCert string

	// ClientKey is the path to the client private key file
	ClientKey string

	// CACert is the path to the CA certificate file
	CACert string

	// AllowInsecure allows insecure connections (not recommended)
	AllowInsecure bool

	// WalletName is the default wallet name to use with Dirk
	// Usually "Wallet" or another name configured in Dirk
	WalletName string

	// ConfigDir is the directory where Dirk stores keystores
	// Used for direct file operations when ethdo is not available
	ConfigDir string
}

// Account represents a validator account in Dirk
type Account struct {
	// PublicKey is the BLS public key of the validator
	PublicKey phase0.BLSPubKey

	// Name is a human-readable identifier for the account
	Name string
}
