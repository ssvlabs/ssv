package beacon

import (
	"encoding/hex"
	"fmt"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math"
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

// Equals returns true if the given metadata is equal to current
func (m *ValidatorMetadata) Equals(other *ValidatorMetadata) bool {
	return other != nil &&
		m.Status == other.Status &&
		m.Index == other.Index &&
		m.Balance == other.Balance
}

// Activated returns true if the validator is not unknown. It might be pending activation or active
func (m *ValidatorMetadata) Activated() bool {
	return m.Status.HasActivated()
}

// Exiting returns true if the validator is existing or exited
func (m *ValidatorMetadata) Exiting() bool {
	return m.Status.IsExited() || m.Status.HasExited()
}

// Slashed returns true if the validator is existing or exited due to slashing
func (m *ValidatorMetadata) Slashed() bool {
	return m.Status == v1.ValidatorStateExitedSlashed || m.Status == v1.ValidatorStateActiveSlashed
}

// OnUpdated represents a function to be called once validator's metadata was updated
type OnUpdated func(pk string, meta *ValidatorMetadata)

// UpdateValidatorsMetadata updates validator information for the given public keys
func UpdateValidatorsMetadata(pubKeys [][]byte, collection ValidatorMetadataStorage, bc Beacon, onUpdated OnUpdated) error {
	logger := logex.GetLogger(zap.String("who", "UpdateValidatorsMetadata"))

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
		if onUpdated != nil {
			onUpdated(pk, meta)
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
	logger.Debug("fetching metadata for public keys", zap.Int("total", len(pubkeys)))
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

// UpdateValidatorsMetadataBatch updates the given public keys in batches
func UpdateValidatorsMetadataBatch(pubKeys [][]byte,
	queue tasks.Queue,
	collection ValidatorMetadataStorage,
	bc Beacon,
	onUpdated OnUpdated,
	batchSize int) {

	batches := int(math.Ceil(float64(len(pubKeys)) / float64(batchSize)))
	start := 0
	end := batchSize

	batchTask := func(pks [][]byte) func() error {
		return func() error {
			return UpdateValidatorsMetadata(pks, collection, bc, onUpdated)
		}
	}

	for i := 0; i <= batches; i++ {
		// run task
		queue.QueueDistinct(batchTask(pubKeys[start:end]), fmt.Sprintf("batch_%d", i))
		// reset start and end
		start = end
		end = int(math.Min(float64(len(pubKeys)), float64(start+batchSize)))
	}
}
