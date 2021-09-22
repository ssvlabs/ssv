package beacon

import (
	"encoding/hex"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ValidatorMetadataStorage interface for validator metadata
type ValidatorMetadataStorage interface {
	UpdateValidatorMetadata(pk string, metadata *ValidatorMetadata) error
}

// ValidatorMetadata represents validator metdata from beacon
type ValidatorMetadata struct {
	Balance spec.Gwei           `json:"balance"`
	Status  v1.ValidatorState   `json:"status"`
	Index   spec.ValidatorIndex `json:"index"` // pointer in order to support nil
}

// Deposited returns true if the validator is not unknown. It might be pending activation or active
func (m *ValidatorMetadata) Deposited() bool {
	return m.Status != v1.ValidatorStateUnknown
}

// Exiting returns true if the validator is existing or exited
func (m *ValidatorMetadata) Exiting() bool {
	return m.Status.IsExited()
}

// Slashed returns true if the validator is existing or exited due to slashing
func (m *ValidatorMetadata) Slashed() bool {
	return m.Status == v1.ValidatorStateExitedSlashed || m.Status == v1.ValidatorStateActiveSlashed
}

// UpdateValidatorsMetadata updates validator information for the given public keys
func UpdateValidatorsMetadata(pubKeys [][]byte, collection ValidatorMetadataStorage, bc Beacon) error {
	logger := logex.GetLogger(zap.String("who", "FetchValidatorsMetadata"))

	results, err := FetchValidatorsMetadata(bc, pubKeys)
	if err != nil {
		return errors.Wrap(err, "failed to get validator data from Beacon")
	}
	logger.Debug("got validators metadata", zap.Int("pks count", len(pubKeys)),
		zap.Int("results count", len(results)))

	var errs []error
	for pk, meta := range results {
		if err := collection.UpdateValidatorMetadata(pk, meta); err != nil {
			logger.Error("failed to update validator metadata",
				zap.String("pk", pk), zap.Error(err))
			errs = append(errs, err)
		}
		logger.Debug("managed to update validator metadata",
			zap.String("pk", pk), zap.Any("metadata", *meta))
	}
	if len(errs) > 0 {
		logger.Error("could not process validators returned from beacon",
			zap.Int("count", len(errs)), zap.Any("errs", errs))
		return errors.Errorf("could not process %d validators returned from beacon", len(errs))
	}

	return nil
}

// FetchValidatorsMetadata is fetching validators data from beacon
func FetchValidatorsMetadata(bc Beacon, pubKeys [][]byte) (map[string]*ValidatorMetadata, error) {
	logger := logex.GetLogger(zap.String("who", "FetchValidatorsMetadata"))
	if len(pubKeys) == 0 {
		return nil, nil
	}
	var pubkeys []spec.BLSPubKey
	for _, pk := range pubKeys {
		blsPubKey := spec.BLSPubKey{}
		copy(blsPubKey[:], pk)
		pubkeys = append(pubkeys, blsPubKey)
	}
	logger.Debug("fetching indices for public keys", zap.Int("total", len(pubkeys)),
		zap.Any("pubkeys", pubkeys))
	validatorsIndexMap, err := bc.GetValidatorData(pubkeys)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get validators data from beacon")
	}
	logger.Debug("got validators metadata", zap.Int("pks count", len(pubKeys)),
		zap.Int("results count", len(validatorsIndexMap)))
	ret := make(map[string]*ValidatorMetadata)
	for index, v := range validatorsIndexMap {
		pk := hex.EncodeToString(v.Validator.PublicKey[:])
		meta := &ValidatorMetadata{
			Balance: v.Balance,
			Status:  v.Status,
			Index:   v.Index,
		}
		ret[pk] = meta
		// once fetched, the internal map in go-client should be updated
		bc.ExtendIndexMap(index, v.Validator.PublicKey)
	}
	return ret, nil
}