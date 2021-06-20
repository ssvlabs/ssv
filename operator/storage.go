package operator

import (
	"crypto/rsa"
	"encoding/base64"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math/big"
)

var (
	prefix        = []byte("operator-")
	syncOffsetKey = []byte("syncOffset")
)

// Storage represents the interface for ssv node storage
type Storage interface {
	eth1.SyncOffsetStorage

	GetPrivateKey() (*rsa.PrivateKey, error)
	SetupPrivateKey(operatorKey string) error
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

// SaveSyncOffset saves the offset
func (s *storage) SaveSyncOffset(offset *eth1.SyncOffset) error {
	return s.db.Set(prefix, syncOffsetKey, offset.Bytes())
}

// GetSyncOffset returns the offset
func (s *storage) GetSyncOffset() (*eth1.SyncOffset, error) {
	obj, err := s.db.Get(prefix, syncOffsetKey)
	if err != nil {
		return nil, err
	}
	offset := new(big.Int)
	offset.SetBytes(obj.Value)
	return offset, nil
}

// GetPrivateKey return rsa private key
func (s *storage) GetPrivateKey() (*rsa.PrivateKey, error) {
	obj, err := s.db.Get(prefix, []byte("private-key"))
	if err != nil {
		return nil, err
	}
	sk, err := rsaencryption.ConvertPemToPrivateKey(string(obj.Value))
	if err != nil {
		return nil, err
	}
	return sk, nil
}

// SetupPrivateKey setup operator private key at the init of the node and set OperatorPublicKey config
func (s *storage) SetupPrivateKey(operatorKeyBase64 string) error {
	operatorKeyByte, err := base64.StdEncoding.DecodeString(operatorKeyBase64)
	if err != nil {
		return errors.Wrap(err, "Failed to decode base64")
	}
	var operatorKey = string(operatorKeyByte)

	newSk, err := s.verifyPrivateKeyExist(operatorKey)
	if err != nil {
		return errors.Wrap(err, "failed to verify operator private key")
	}
	if operatorKey != "" || newSk != "" {
		if newSk != "" { // newly sk generated, need to set the operator key
			operatorKey = newSk
		}

		if err := s.savePrivateKey(operatorKey); err != nil {
			return errors.Wrap(err, "failed to save operator private key")
		}
	}

	sk, err := s.GetPrivateKey()
	if err != nil {
		return errors.Wrap(err, "failed to get operator private key")
	}
	operatorPublicKey, err := rsaencryption.ExtractPublicKey(sk)
	if err != nil {
		return errors.Wrap(err, "failed to extract operator public key")
	}
	s.logger.Info("operator public key", zap.Any("key", operatorPublicKey))
	params.SsvConfig().OperatorPublicKey = operatorPublicKey
	return nil
}

// SavePrivateKey save operator private key
func (s *storage) savePrivateKey(operatorKey string) error {
	if err := s.db.Set(prefix, []byte("private-key"), []byte(operatorKey)); err != nil {
		return err
	}
	return nil
}

// verifyPrivateKeyExist return true if key exist and no new key passed else return new generated key
func (s *storage) verifyPrivateKeyExist(operatorKey string) (string, error) {
	// check if sk is exist or passedKey is passed. if not, generate new operator key
	if _, err := s.GetPrivateKey(); err != nil { // need to generate new operator key
		if err.Error() == kv.EntryNotFoundError && operatorKey == "" {
			_, skByte, err := rsaencryption.GenerateKeys()
			if err != nil {
				return "", errors.Wrap(err, "failed to generate new keys")
			}
			return string(skByte), nil // new key generated
		} else if err.Error() != kv.EntryNotFoundError {
			return "", errors.Wrap(err, "failed to get private key")
		}
	}
	return "", nil // key already exist, no need to return sk
}
