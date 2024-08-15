package beacon

import (
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

//go:generate mockgen -package=beacon -destination=./mock_validator_metadata.go -source=./validator_metadata.go

// ValidatorMetadataStorage interface for validator metadata
type ValidatorMetadataStorage interface {
	UpdateValidatorMetadata(pk spectypes.ValidatorPK, metadata *ValidatorMetadata) error
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*ValidatorMetadata) error
}

// ValidatorMetadata represents validator metdata from beacon
type ValidatorMetadata struct {
	Balance         phase0.Gwei              `json:"balance"`
	Status          eth2apiv1.ValidatorState `json:"status"`
	Index           phase0.ValidatorIndex    `json:"index"` // pointer in order to support nil
	ActivationEpoch phase0.Epoch             `json:"activation_epoch"`
}

// Equals returns true if the given metadata is equal to current
func (m *ValidatorMetadata) Equals(other *ValidatorMetadata) bool {
	return other != nil &&
		m.Status == other.Status &&
		m.Index == other.Index &&
		m.Balance == other.Balance &&
		m.ActivationEpoch == other.ActivationEpoch
}

// Pending returns true if the validator is pending
func (m *ValidatorMetadata) Pending() bool {
	return m.Status.IsPending()
}

// Activated returns true if the validator is not unknown. It might be pending activation or active
func (m *ValidatorMetadata) Activated() bool {
	return m.Status.HasActivated() || m.Status.IsActive() || m.Status.IsAttesting()
}

// IsActive returns true if the validator is currently active. Cant be other state
func (m *ValidatorMetadata) IsActive() bool {
	return m.Status == eth2apiv1.ValidatorStateActiveOngoing
}

// IsAttesting returns true if the validator should be attesting.
func (m *ValidatorMetadata) IsAttesting() bool {
	return m.Status.IsAttesting()
}

// Exiting returns true if the validator is existing or exited
func (m *ValidatorMetadata) Exiting() bool {
	return m.Status.IsExited() || m.Status.HasExited()
}

// Slashed returns true if the validator is existing or exited due to slashing
func (m *ValidatorMetadata) Slashed() bool {
	return m.Status == eth2apiv1.ValidatorStateExitedSlashed || m.Status == eth2apiv1.ValidatorStateActiveSlashed
}

// OnUpdated represents a function to be called once validator's metadata was updated
type OnUpdated func(data map[spectypes.ValidatorPK]*ValidatorMetadata) error

// UpdateValidatorsMetadata updates validator information for the given public keys
func UpdateValidatorsMetadata(logger *zap.Logger, pubKeys [][]byte, bc BeaconNode, onUpdated OnUpdated) error {
	start := time.Now()
	results, err := FetchValidatorsMetadata(bc, pubKeys)
	if err != nil {
		return errors.Wrap(err, "failed to get validator data from Beacon")
	}
	logger.Debug("⏱️ fetched validators metadata", zap.Duration("elapsed", time.Since(start)))
	// TODO: importing logging/fields causes import cycle
	logger.Debug("🆕 got validators metadata", zap.Int("requested", len(pubKeys)),
		zap.Int("received", len(results)))
	if err = onUpdated(results); err != nil {
		logger.Warn("❗ failed to call UpdateValidatorsMetadata onUpdated callback",
			zap.Error(err))
	}

	return nil
}

// FetchValidatorsMetadata is fetching validators data from beacon
func FetchValidatorsMetadata(bc BeaconNode, pubKeys [][]byte) (map[spectypes.ValidatorPK]*ValidatorMetadata, error) {
	if len(pubKeys) == 0 {
		return nil, nil
	}
	var pubkeys []phase0.BLSPubKey
	for _, pk := range pubKeys {
		blsPubKey := phase0.BLSPubKey{}
		copy(blsPubKey[:], pk)
		pubkeys = append(pubkeys, blsPubKey)
	}
	validatorsIndexMap, err := bc.GetValidatorData(pubkeys)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get validators data from beacon")
	}
	ret := make(map[spectypes.ValidatorPK]*ValidatorMetadata)
	for _, v := range validatorsIndexMap {
		meta := &ValidatorMetadata{
			Balance:         v.Balance,
			Status:          v.Status,
			Index:           v.Index,
			ActivationEpoch: v.Validator.ActivationEpoch,
		}
		ret[spectypes.ValidatorPK(v.Validator.PublicKey)] = meta
	}
	return ret, nil
}
