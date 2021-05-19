package collections

import (
	"crypto/rsa"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"go.uber.org/zap"
)

// IOperatorStorage interface that managing all operator settings
type IOperatorStorage interface {
	SavePrivateKey(operatorKey string) error
	GetPrivateKey() (*rsa.PrivateKey, error)
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

// SavePrivateKey save operator private key
func (o OperatorStorage) SavePrivateKey(operatorKey string) error {
	if err := o.db.Set(o.prefix, []byte("private-key"), []byte(operatorKey)); err != nil{
		return err
	}
	return nil
}

// GetPrivateKey return rsa private key
func (o OperatorStorage) GetPrivateKey() (*rsa.PrivateKey, error){
	obj, err := o.db.Get(o.prefix, []byte("private-key"))
	if err != nil{
		return nil, err
	}
	sk, err := rsaencryption.ConvertPemToPrivateKey(string(obj.Value))
	if err != nil{
		return nil, err
	}
	return sk, nil
}