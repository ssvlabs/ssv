package operator

import (
	"crypto/rsa"
	"encoding/base64"
	"math/big"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	prefix        = []byte("operator-")
	syncOffsetKey = []byte("syncOffset")
)

// Storage represents the interface for ssv node storage
type Storage interface {
	eth1.SyncOffsetStorage
	basedb.RegistryStore

	GetPrivateKey() (*rsa.PrivateKey, bool, error)
	SetupPrivateKey(generateIfNone bool, operatorKeyBase64 string) error
}

type storage struct {
	db     basedb.IDb
	logger *zap.Logger
}

// NewOperatorNodeStorage creates a new instance of Storage
func NewOperatorNodeStorage(db basedb.IDb, logger *zap.Logger) Storage {
	es := storage{db, logger}
	return &es
}

func (s *storage) CleanRegistryData() error {
	return s.cleanSyncOffset()
}

// SaveSyncOffset saves the offset
func (s *storage) SaveSyncOffset(offset *eth1.SyncOffset) error {
	return s.db.Set(prefix, syncOffsetKey, offset.Bytes())
}

func (s *storage) cleanSyncOffset() error {
	return s.db.RemoveAllByCollection(append(prefix, syncOffsetKey...))
}

// GetSyncOffset returns the offset
func (s *storage) GetSyncOffset() (*eth1.SyncOffset, bool, error) {
	obj, found, err := s.db.Get(prefix, syncOffsetKey)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	offset := new(big.Int)
	offset.SetBytes(obj.Value)
	return offset, found, nil
}

// GetPrivateKey return rsa private key
func (s *storage) GetPrivateKey() (*rsa.PrivateKey, bool, error) {
	obj, found, err := s.db.Get(prefix, []byte("private-key"))
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, found, nil
	}
	sk, err := rsaencryption.ConvertPemToPrivateKey(string(obj.Value))
	if err != nil {
		return nil, false, err
	}
	return sk, found, nil
}

// SetupPrivateKey setup operator private key at the init of the node and set OperatorPublicKey config
func (s *storage) SetupPrivateKey(generateIfNone bool, operatorKeyBase64 string) error {
	logger := s.logger.With(zap.String("who", "operatorKeys"))
	operatorKeyByte, err := base64.StdEncoding.DecodeString(operatorKeyBase64)
	if err != nil {
		return errors.Wrap(err, "Failed to decode base64")
	}
	var operatorKey = string(operatorKeyByte)

	if err := s.validateKey(generateIfNone, operatorKey); err != nil {
		return err
	}

	sk, found, err := s.GetPrivateKey()
	if err != nil {
		return errors.Wrap(err, "failed to get operator private key")
	}
	if !found {
		return errors.New("failed to find operator private key")
	}
	operatorPublicKey, err := rsaencryption.ExtractPublicKey(sk)
	if err != nil {
		return errors.Wrap(err, "failed to extract operator public key")
	}
	logger.Info("setup operator privateKey is DONE!", zap.Any("public-key", operatorPublicKey))
	return nil
}

// validateKey validate provided and exist key. save if needed.
func (s *storage) validateKey(generateIfNone bool, operatorKey string) error {
	// check if passed new key. if so, save new key (force to always save key when provided)
	if operatorKey != "" {
		return s.savePrivateKey(operatorKey)
	}
	// new key not provided, check if key exist
	_, found, err := s.GetPrivateKey()
	if err != nil {
		return err
	}
	// if no, check if need to generate. if no, return error
	if !found {
		if !generateIfNone {
			return errors.New("key not exist or provided")
		}
		_, skByte, err := rsaencryption.GenerateKeys()
		if err != nil {
			return errors.WithMessage(err, "failed to generate new key")
		}
		return s.savePrivateKey(string(skByte))
	}

	// key exist in storage.
	return nil
}

// SavePrivateKey save operator private key
func (s *storage) savePrivateKey(operatorKey string) error {
	if err := s.db.Set(prefix, []byte("private-key"), []byte(operatorKey)); err != nil {
		return err
	}
	return nil
}
