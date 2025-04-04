package dirk

import (
	"context"
	"errors"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

var (
	ErrKeyDeletionNotSupported = errors.New("key deletion is not supported by Dirk")
)

// Signer implements the signing functionality for Dirk remote signer
type Signer struct {
	endpoint    string
	credentials Credentials
	client      Client
}

// Client defines the interface for communication with a Dirk server
type Client interface {
	// ListAccounts returns all accounts available in the Dirk server
	ListAccounts() ([]Account, error)

	// Sign signs data using the specified account
	// ctx: The context for the request
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
}

// Account represents a validator account in Dirk
type Account struct {
	// PublicKey is the BLS public key of the validator
	PublicKey phase0.BLSPubKey

	// Name is a human-readable identifier for the account
	Name string
}
