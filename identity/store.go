package p2p

import (
	"crypto/ecdsa"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	// TODO: use the new prefixes and add migration
	// prefix is the store prefix
	prefix = []byte("p2p-")
	// prefix = []byte("identity/")
	// netKeyPrefix is the prefix for network key
	netKeyPrefix = []byte("private-key")
	// netKeyPrefix = []byte("network-key/")
	// operatorKeyPrefix is the prefix for operator key
	// operatorKeyPrefix = []byte("operator-key/")
)

// Store represents the interface for accessing the node's keys (operator and network keys)
// TODO: add operator key
type Store interface {
	GetNetworkKey() (*ecdsa.PrivateKey, bool, error)
	SetupNetworkKey(logger *zap.Logger, skEncoded string) (*ecdsa.PrivateKey, error)
	// GetOperatorKey() (*rsa.PrivateKey, bool, error)
	// SetupOperatorkKey(skEncoded string) (*rsa.PrivateKey, error)
}

type identityStore struct {
	db basedb.Database
}

// NewIdentityStore creates a new identity store
func NewIdentityStore(db basedb.Database) Store {
	es := identityStore{db}
	return &es
}

func (s identityStore) GetNetworkKey() (*ecdsa.PrivateKey, bool, error) {
	obj, found, err := s.db.Get(prefix, netKeyPrefix)
	if !found {
		return nil, false, nil
	}
	if err != nil {
		return nil, found, err
	}
	pk, err := decode(obj.Value)
	pk.Curve = gcrypto.S256() // temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1
	if err != nil {
		return nil, found, errors.WithMessage(err, "failed to decode private key")
	}
	return pk, found, nil
}

func (s identityStore) SetupNetworkKey(logger *zap.Logger, skEncoded string) (*ecdsa.PrivateKey, error) {
	privateKey, found, err := s.GetNetworkKey()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get privateKey")
	}
	if skEncoded == "" && found && privateKey != nil {
		logger.Debug("using p2p network privateKey from storage")
		return privateKey, nil
	}
	privateKey, err = utils.ECDSAPrivateKey(logger.Named(logging.NameP2PStorage), skEncoded)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to generate private key")
	}

	return privateKey, s.saveNetworkKey(privateKey)
}

func (s identityStore) saveNetworkKey(privateKey *ecdsa.PrivateKey) error {
	if err := s.db.Set(prefix, netKeyPrefix, encode(privateKey)); err != nil {
		return errors.WithMessage(err, "failed to save to db")
	}
	return nil
}

func encode(privateKey *ecdsa.PrivateKey) []byte {
	return gcrypto.FromECDSA(privateKey)
}

func decode(b []byte) (*ecdsa.PrivateKey, error) {
	return gcrypto.ToECDSA(b)
}
