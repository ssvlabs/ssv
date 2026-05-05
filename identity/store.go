package p2p

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"

	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils"
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
	encryptedPrefix = []byte("enc:")
)

// Store represents the interface for accessing the node's keys (operator and network keys)
type Store interface {
	GetNetworkKey(ctx context.Context) (*ecdsa.PrivateKey, bool, error)
	SetupNetworkKey(ctx context.Context, skEncoded string) (*ecdsa.PrivateKey, error)
}

type identityStore struct {
	logger      *zap.Logger
	db          basedb.Database
	protectFn   func(ctx context.Context, plaintext []byte) ([]byte, error)
	unprotectFn func(ctx context.Context, protectedValue []byte) ([]byte, error)
}

// NewIdentityStore creates a new identity store
func NewIdentityStore(
	logger *zap.Logger,
	db basedb.Database,
	protectFn func(ctx context.Context, plaintext []byte) ([]byte, error),
	unprotectFn func(ctx context.Context, protectedValue []byte) ([]byte, error),
) Store {
	es := identityStore{
		logger:      logger.Named(log.NameP2PStorage),
		db:          db,
		protectFn:   protectFn,
		unprotectFn: unprotectFn,
	}
	return &es
}

func (s identityStore) GetNetworkKey(ctx context.Context) (*ecdsa.PrivateKey, bool, error) {
	obj, found, err := s.db.Get(prefix, netKeyPrefix)
	if err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	pk, _, err := decodeNetworkKey(ctx, obj.Value, s.unprotectFn)
	if err != nil {
		return nil, found, fmt.Errorf("failed to decode private key: %w", err)
	}
	pk.Curve = gcrypto.S256() // temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1
	return pk, found, nil
}

func (s identityStore) SetupNetworkKey(ctx context.Context, skEncoded string) (*ecdsa.PrivateKey, error) {
	var (
		privateKey *ecdsa.PrivateKey
		found      bool
		encrypted  bool
		err        error
	)
	if skEncoded == "" {
		obj, keyFound, err := s.db.Get(prefix, netKeyPrefix)
		if err != nil {
			return nil, fmt.Errorf("failed to get privateKey: %w", err)
		}
		if keyFound {
			privateKey, encrypted, err = decodeNetworkKey(ctx, obj.Value, s.unprotectFn)
			if err != nil {
				return nil, fmt.Errorf("decode network key: %w", err)
			}
			privateKey.Curve = gcrypto.S256() // temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1
			found = true
		}
	}
	if skEncoded == "" && found && privateKey != nil {
		if !encrypted {
			if s.hasProtectedStorage() {
				s.logger.Info("migrating plaintext p2p network private key to encrypted storage")
				if err := s.saveNetworkKey(ctx, privateKey); err != nil {
					return nil, err
				}
			} else {
				s.logger.Warn("using legacy plaintext p2p network private key from storage; configure a local operator key or use an ssv-signer deployment that supports remote network-key protection to encrypt it at rest")
			}
		}
		s.logger.Debug("using p2p network privateKey from storage")
		return privateKey, nil
	}
	privateKey, err = utils.ECDSAPrivateKey(s.logger, skEncoded)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	if !s.hasProtectedStorage() {
		s.logger.Warn("persisting p2p network private key in legacy plaintext storage because no network key encryption secret is configured; configure a local operator key or use an ssv-signer deployment that supports remote network-key protection to encrypt it at rest")
		return privateKey, s.saveNetworkKeyPlaintext(privateKey)
	}

	return privateKey, s.saveNetworkKey(ctx, privateKey)
}

func (s identityStore) saveNetworkKey(ctx context.Context, privateKey *ecdsa.PrivateKey) error {
	protectedValue, err := encodeNetworkKey(ctx, privateKey, s.protectFn)
	if err != nil {
		return fmt.Errorf("failed to protect private key: %w", err)
	}
	if err := s.db.Set(prefix, netKeyPrefix, protectedValue); err != nil {
		return fmt.Errorf("failed to save to db: %w", err)
	}
	return nil
}

func (s identityStore) hasProtectedStorage() bool {
	return s.protectFn != nil && s.unprotectFn != nil
}

func (s identityStore) saveNetworkKeyPlaintext(privateKey *ecdsa.PrivateKey) error {
	if err := s.db.Set(prefix, netKeyPrefix, gcrypto.FromECDSA(privateKey)); err != nil {
		return fmt.Errorf("failed to save to db: %w", err)
	}
	return nil
}

// HasEncryptedNetworkKey reports whether the stored network key already uses the
// encrypted-at-rest storage format.
func HasEncryptedNetworkKey(db basedb.Database) (bool, error) {
	obj, found, err := db.Get(prefix, netKeyPrefix)
	if err != nil || !found {
		return false, err
	}

	return bytes.HasPrefix(obj.Value, encryptedPrefix), nil
}

func encodeNetworkKey(
	ctx context.Context,
	privateKey *ecdsa.PrivateKey,
	protectFn func(ctx context.Context, plaintext []byte) ([]byte, error),
) ([]byte, error) {
	plaintext := gcrypto.FromECDSA(privateKey)
	if protectFn == nil {
		return nil, errors.New("network key protector is required")
	}
	protectedValue, err := protectFn(ctx, plaintext)
	if err != nil {
		return nil, err
	}
	storedValue := make([]byte, 0, len(encryptedPrefix)+len(protectedValue))
	storedValue = append(storedValue, encryptedPrefix...)
	storedValue = append(storedValue, protectedValue...)
	return storedValue, nil
}

func decodeNetworkKey(
	ctx context.Context,
	storedValue []byte,
	unprotectFn func(ctx context.Context, protectedValue []byte) ([]byte, error),
) (*ecdsa.PrivateKey, bool, error) {
	if bytes.HasPrefix(storedValue, encryptedPrefix) {
		if unprotectFn == nil {
			return nil, true, errors.New("network key is encrypted but no compatible network key protector is configured")
		}
		decrypted, err := unprotectFn(ctx, storedValue[len(encryptedPrefix):])
		if err != nil {
			return nil, true, err
		}
		pk, err := gcrypto.ToECDSA(decrypted)
		return pk, true, err
	}

	pk, err := gcrypto.ToECDSA(storedValue)
	return pk, false, err
}
