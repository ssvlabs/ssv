package collections

import (
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/collections/interfaces"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)



// ValidatorStorage struct
type ValidatorStorage struct {
	prefix []byte
	db     storage.Db
	logger *zap.Logger
}

// NewValidator creates new validator storage
func NewValidator(db storage.Db, logger *zap.Logger) ValidatorStorage {
	validator := ValidatorStorage{
		prefix: []byte("validator"),
		db:     db,
		logger: logger,
	}
	return validator
}


// LoadFromConfig fetch validator share form .env and save it to db
func (v *ValidatorStorage) LoadFromConfig(validator *interfaces.Validator){
	err := v.SaveValidatorShare(validator)
	if err != nil {
		v.logger.Error("Failed to load validator share data from config", zap.Error(err))
	}
}

// SaveValidatorShare save validator share to db
func (v *ValidatorStorage) SaveValidatorShare(validator *interfaces.Validator) error{
	value, err := validator.Serialize()
	if err != nil {
		v.logger.Error("failed serialized validator", zap.Error(err))
	}
	return v.db.Set(v.prefix, validator.PubKey.Serialize(), value)
}

// GetAllValidatorsShare returns ALL validators shares from db
func (v *ValidatorStorage) GetAllValidatorsShare() ([]*interfaces.Validator, error){
	objs, err := v.db.GetAllByBucket(v.prefix)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get val share")
	}
	var res []*interfaces.Validator
	for _, obj := range objs {
		val, err := (&interfaces.Validator{}).Deserialize(obj)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to deserialized validator")
		}
		res = append(res, val)
	}

	return res, nil
}


