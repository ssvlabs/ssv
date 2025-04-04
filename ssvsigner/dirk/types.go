package dirk

import (
	"errors"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

var (
	ErrKeyDeletionNotSupported = errors.New("key deletion is not supported by Dirk")
)

// Signer implements the RemoteSigner interface for Dirk
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
	Sign(pubKey []byte, data []byte, domain []byte) ([]byte, error)

	// Close closes the connection to Dirk
	Close() error
}

// Credentials holds authentication information for Dirk
type Credentials struct {
	ClientCert    string
	ClientKey     string
	CACert        string
	AllowInsecure bool // TODO: sounds retarded
}

// Account represents a validator account in Dirk
type Account struct {
	// PublicKey is the BLS public key of the validator
	PublicKey phase0.BLSPubKey

	// Path is the derivation path for HD wallets
	Path string

	// Name is a human-readable identifier for the account
	Name string
}
