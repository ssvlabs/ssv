package collections

import (
	"bytes"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/dgraph-io/badger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// IOperatorStorage interface that managing all operator settings
type IOperatorStorage interface {
	GetPrivateKey() (*rsa.PrivateKey, error)
	SetupPrivateKey(operatorKey string) error
	GetOperatorInformation(operatorPubKey []byte) (*OperatorInformation, error)
	SaveOperatorInformation(operatorInformation *OperatorInformation) error
}

// OperatorInformation the public data of an operator
type OperatorInformation struct {
	PublicKey    []byte
	Name         string
	OwnerAddress common.Address
	Index        int
}

// OperatorStorage implement IOperatorStorage
type OperatorStorage struct {
	prefix []byte
	db     basedb.IDb
	logger *zap.Logger
}

// NewOperatorStorage init new instance of operator storage
func NewOperatorStorage(db basedb.IDb, logger *zap.Logger) IOperatorStorage {
	validator := OperatorStorage{
		prefix: []byte("operator-"),
		db:     db,
		logger: logger,
	}
	return &validator
}

// GetPrivateKey return rsa private key
func (o *OperatorStorage) GetPrivateKey() (*rsa.PrivateKey, error) {
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
func (o *OperatorStorage) SetupPrivateKey(operatorKeyBase64 string) error {
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

// ListOperators returns information of all the known operators
func (o *OperatorStorage) ListOperators() ([]OperatorInformation, error) {
	objs, err := o.db.GetAllByCollection(o.prefix)
	if err != nil {
		return nil, err
	}
	var operators []OperatorInformation
	privKeyBytes := append(o.prefix, []byte("private-key")...)
	for _, obj := range objs {
		if !bytes.Equal(obj.Key, privKeyBytes) {
			var operatorInformation OperatorInformation
			err = json.Unmarshal(obj.Value, &operatorInformation)
			operators = append(operators, operatorInformation)
		}
	}

	return operators, err
}

// GetOperatorInformation returns information of the given operator by public key
func (o *OperatorStorage) GetOperatorInformation(operatorPubKey []byte) (*OperatorInformation, error) {
	obj, err := o.db.Get(o.prefix, operatorKey(operatorPubKey))
	if err != nil {
		return nil, err
	}
	var operatorInformation OperatorInformation
	err = json.Unmarshal(obj.Value, &operatorInformation)
	return &operatorInformation, err
}

// SaveOperatorInformation saves operator information by its public key
func (o *OperatorStorage) SaveOperatorInformation(operatorInformation *OperatorInformation) error {
	raw, err := json.Marshal(operatorInformation)
	if err != nil {
		return errors.Wrap(err, "could not marshal operator information")
	}
	return o.db.Set(o.prefix, operatorKey(operatorInformation.PublicKey), raw)
}

// SavePrivateKey save operator private key
func (o *OperatorStorage) savePrivateKey(operatorKey string) error {
	if err := o.db.Set(o.prefix, []byte("private-key"), []byte(operatorKey)); err != nil {
		return err
	}
	return nil
}

// verifyPrivateKeyExist return true if key exist and no new key passed else return new generated key
func (o *OperatorStorage) verifyPrivateKeyExist(operatorKey string) (string, error) {
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

func operatorKey(pubKey []byte) []byte {
	return bytes.Join([][]byte{
		[]byte("operator/"),
		pubKey[:],
	}, []byte("/"))
}
