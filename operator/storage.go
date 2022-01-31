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
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	sk, err := rsaencryption.ConvertPemToPrivateKey(string(obj.Value))
	if err != nil {
		return nil, found, err
	}
	return sk, found, nil
}

// SetupPrivateKey setup operator private key at the init of the node and set OperatorPublicKey config
func (s *storage) SetupPrivateKey(operatorKeyBase64 string) error {
	logger := s.logger.With(zap.String("who", "operatorKeys"))
	operatorKeyByte, err := base64.StdEncoding.DecodeString(operatorKeyBase64)
	if err != nil {
		return errors.Wrap(err, "Failed to decode base64")
	}
	var operatorKey = string(operatorKeyByte)

	newSk, exist, err := s.verifyPrivateKeyExistAndGenerateIfNeed(operatorKey)
	if err != nil {
		return errors.Wrap(err, "failed to verify operator private key")
	}
	if newSk != "" {
		logger.Info("using newly generated operator key")
	} else {
		if exist {
			if operatorKey != "" {
				newSk = operatorKey
				logger.Info("using key from config, overriding existing key")
			} else {
				logger.Info("using exist operator key from storage")
			}
		} else {
			newSk = operatorKey
			logger.Info("using operator key from config")
		}
	}
	if newSk != "" {
		if err := s.savePrivateKey(newSk); err != nil {
			return errors.Wrap(err, "failed to save operator private key")
		}
	}

	sk, found, err := s.GetPrivateKey()
	if !found {
		return errors.New("failed to find operator private key")
	}
	if err != nil {
		return errors.Wrap(err, "failed to get operator private key")
	}
	operatorPublicKey, err := rsaencryption.ExtractPublicKey(sk)
	if err != nil {
		return errors.Wrap(err, "failed to extract operator public key")
	}
	logger.Info("setup operator privateKey is DONE!", zap.Any("public-key", operatorPublicKey))
	return nil
}

// SavePrivateKey save operator private key
func (s *storage) savePrivateKey(operatorKey string) error {
	if err := s.db.Set(prefix, []byte("private-key"), []byte(operatorKey)); err != nil {
		return err
	}
	return nil
}

// verifyPrivateKeyExistAndGenerateIfNeed return true if key exist and no new key passed else return new generated key
func (s *storage) verifyPrivateKeyExistAndGenerateIfNeed(operatorKey string) (string, bool, error) {
	_, found, err := s.GetPrivateKey()
	if !found && operatorKey == "" { // no sk in storage and no key passed from config
		_, skByte, err := rsaencryption.GenerateKeys()
		if err != nil {
			return "", false, errors.Wrap(err, "failed to generate new keys")
		}
		return string(skByte), false, nil // new key generated
	}
	if err != nil { // need to generate new operator key
		return "", false, errors.Wrap(err, "failed to get private key")
	}
	return "", found, nil // key already exist or key passed from config, no need to return newly generated sk
}
