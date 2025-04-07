package dirk

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Client defines the interface for interacting with a Dirk signing service.
// Implementations could use gRPC (like GRPCClient) or potentially other protocols.
// It focuses on core signing and account listing operations provided by Dirk.
type Client interface {
	// ListAccounts retrieves a list of accounts (public keys and names)
	// known to the connected Dirk instance.
	ListAccounts(ctx context.Context) ([]Account, error)

	// Sign requests the Dirk instance to sign the provided data (typically a signing root)
	// using the key share associated with the given public key.
	// - ctx: Context for the gRPC call or other communication method.
	// - pubKey: The BLS public key (48 bytes) identifying the account/key share.
	// - data: The 32-byte data (signing root) to be signed.
	// Returns the 96-byte BLS signature or an error.
	Sign(ctx context.Context, pubKey []byte, data []byte) ([]byte, error)

	// Close terminates the connection to the Dirk service (e.g., closes the gRPC connection).
	Close() error
}

// Account represents a validator account known by Dirk.
type Account struct {
	// PublicKey is the BLS public key of the validator account/share.
	PublicKey phase0.BLSPubKey

	// Name is the human-readable identifier assigned to the account within Dirk.
	// Format is typically "WalletName/AccountName", e.g., "Wallet/validator-0a1b2c3d".
	Name string
}

// Credentials holds authentication and configuration information for connecting to Dirk
// and potentially for associated tools like ethdo.
type Credentials struct {
	// ClientCert is the path to the client TLS certificate file for mTLS authentication with Dirk.
	ClientCert string

	// ClientKey is the path to the client TLS private key file for mTLS authentication with Dirk.
	ClientKey string

	// CACert is the path to the Certificate Authority (CA) certificate file used to verify the Dirk server's certificate.
	CACert string

	// AllowInsecure allows insecure gRPC connections (disables TLS). Not recommended for production.
	AllowInsecure bool

	// WalletName specifies the target wallet name within Dirk, often used during key imports via ethdo.
	// Defaults typically to "Wallet".
	WalletName string

	// EthdoBaseDir optionally specifies the base directory for ethdo operations via the --base-dir flag.
	// This might correspond to Dirk's configuration/data directory if ethdo needs to interact with it directly (e.g., for certain wallet types).
	EthdoBaseDir string
}
