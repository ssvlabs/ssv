package collections

import (
	"crypto/rsa"
	"encoding/base64"
	"github.com/dgraph-io/badger"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// IOperatorStorage interface that managing all operator settings
type IOperatorStorage interface {
	GetPrivateKey() (*rsa.PrivateKey, error)
	SetupPrivateKey(operatorKey string) error
}

// OperatorStorage implement IOperatorStorage
type OperatorStorage struct {
	prefix []byte
	db     storage.IKvStorage
	logger *zap.Logger
	pubsub.BaseObserver
}

// NewOperatorStorage init new instance of operator storage
func NewOperatorStorage(db storage.IKvStorage, logger *zap.Logger) OperatorStorage {
	validator := OperatorStorage{
		prefix: []byte("operator-"),
		db:     db,
		logger: logger,
	}
	return validator
}

// GetPrivateKey return rsa private key
func (o OperatorStorage) GetPrivateKey() (*rsa.PrivateKey, error) {
	obj, err := o.db.Get(o.prefix, []byte("private-key"))
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
func (o OperatorStorage) SetupPrivateKey(operatorKeyBase64 string) error {
	operatorKeyByte, err := base64.StdEncoding.DecodeString(operatorKeyBase64)
	if err != nil {
		return errors.Wrap(err, "Failed to decode base64")
	}
	var operatorKey = string(operatorKeyByte)

	newSk, err := o.verifyPrivateKeyExist(operatorKey)
	if err != nil {
		return errors.Wrap(err, "failed to verify operator private key")
	}
	if operatorKey != "" || newSk != "" {
		if newSk != "" { // newly sk generated, need to set the operator key
			operatorKey = newSk
		}

		if err := o.savePrivateKey(operatorKey); err != nil {
			return errors.Wrap(err, "failed to save operator private key")
		}
	}

	sk, err := o.GetPrivateKey()
	if err != nil {
		return errors.Wrap(err, "failed to get operator private key")
	}
	operatorPublicKey, err := rsaencryption.ExtractPublicKey(sk)
	if err != nil {
		return errors.Wrap(err, "failed to extract operator public key")
	}
	o.logger.Info("operator public key", zap.Any("key", operatorPublicKey))
	params.SsvConfig().OperatorPublicKey = operatorPublicKey
	return nil
}


// SavePrivateKey save operator private key
func (o OperatorStorage) savePrivateKey(operatorKey string) error {
	if err := o.db.Set(o.prefix, []byte("private-key"), []byte(operatorKey)); err != nil {
		return err
	}
	return nil
}

// verifyPrivateKeyExist return true if key exist and no new key passed else return new generated key
func (o OperatorStorage) verifyPrivateKeyExist(operatorKey string) (string, error) {
	// check if sk is exist or passedKey is passed. if not, generate new operator key
	if _, err := o.GetPrivateKey(); err != nil { // need to generate new operator key
		if err.Error() == badger.ErrKeyNotFound.Error() && operatorKey == "" {
			_, skByte, err := rsaencryption.GenerateKeys()
			if err != nil {
				return "", errors.Wrap(err, "failed to generate new keys")
			}
			return string(skByte), nil // new key generated
		} else if err.Error() != badger.ErrKeyNotFound.Error() {
			return "", errors.Wrap(err, "failed to get private key")
		}
	}
	return "", nil // key already exist, no need to return sk
}
