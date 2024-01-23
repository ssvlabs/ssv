package metadatamanager

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/operator/operatordatastore"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/registry/storage"
)

type MetadataManager struct {
	logger                 *zap.Logger
	metadataLastUpdated    map[string]time.Time
	metadataUpdateInterval time.Duration
	sharesStorage          storage.Shares
	beaconNode             beaconprotocol.BeaconNode
	validatorsMap          *validatorsmap.ValidatorsMap
	network                network.P2PNetwork
	operatorDataStore      operatordatastore.OperatorDataStore
	indicesChangeCh        chan struct{}
}

func New(logger *zap.Logger, sharesStorage storage.Shares, beaconNode beaconprotocol.BeaconNode, validatorsMap *validatorsmap.ValidatorsMap, network network.P2PNetwork, operatorDataStore operatordatastore.OperatorDataStore, indicesChangeCh chan struct{}) *MetadataManager {
	return &MetadataManager{
		logger:                 logger,
		metadataLastUpdated:    make(map[string]time.Time),
		metadataUpdateInterval: 5 * time.Minute,
		sharesStorage:          sharesStorage,
		beaconNode:             beaconNode,
		validatorsMap:          validatorsMap,
		network:                network,
		operatorDataStore:      operatorDataStore,
		indicesChangeCh:        indicesChangeCh,
	}
}

// UpdateValidatorMetaDataIteration updates metadata of validators in an interval
func (mm *MetadataManager) UpdateValidatorMetaDataIteration() (map[phase0.BLSPubKey]*beaconprotocol.ValidatorMetadata, error) {
	shares := mm.sharesStorage.List(nil, storage.ByNotLiquidated(), mm.byNotRecentlyUpdated)
	var pks []spectypes.ValidatorPK
	for _, share := range shares {
		pks = append(pks, share.ValidatorPubKey)
		mm.metadataLastUpdated[string(share.ValidatorPubKey)] = time.Now()
	}

	if len(pks) == 0 {
		return nil, nil
	}

	mm.logger.Debug("updating validators metadata",
		zap.Int("validators", len(shares)))

	return mm.UpdateValidatorsMetadataForPublicKeys(pks)
}

// UpdateValidatorsMetadataForPublicKeys updates validator information for the given public keys
func (mm *MetadataManager) UpdateValidatorsMetadataForPublicKeys(pubKeys []spectypes.ValidatorPK) (map[phase0.BLSPubKey]*beaconprotocol.ValidatorMetadata, error) {
	results, err := mm.fetchValidatorsMetadata(pubKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator data from Beacon: %w", err)
	}

	// TODO: importing logging/fields causes import cycle
	mm.logger.Debug("🆕 got validators metadata", zap.Int("requested", len(pubKeys)),
		zap.Int("received", len(results)))

	var errs []error
	for pk, metadata := range results {
		if err := mm.updateSingleMetadata(pk[:], metadata); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		// TODO: update log
		mm.logger.Error("❌ failed to process validators returned from Beacon node",
			zap.Int("count", len(errs)), zap.Errors("errors", errs))
		return nil, errors.Errorf("could not process %d validators returned from beacon", len(errs))
	}

	return results, nil
}

func (mm *MetadataManager) updateSingleMetadata(pk []byte, metadata *beaconprotocol.ValidatorMetadata) error {
	if metadata == nil {
		return fmt.Errorf("could not update empty metadata")
	}

	mm.metadataLastUpdated[string(pk)] = time.Now()
	pkHex := hex.EncodeToString(pk)

	// Save metadata to share storage.
	err := mm.sharesStorage.UpdateValidatorMetadata(pk, metadata)
	if err != nil {
		return fmt.Errorf("could not update validator metadata: %w", err)
	}

	// If this validator is not ours, don't start it.
	share := mm.sharesStorage.Get(nil, pk)
	if share == nil {
		return errors.New("share was not found")
	}
	if !share.BelongsToOperator(mm.operatorDataStore.GetOperatorData().ID) {
		return nil
	}

	mm.logger.Debug("💾️ successfully updated validator metadata",
		zap.String("pk", pkHex), zap.Any("metadata", metadata))

	return nil
}

// fetchValidatorsMetadata is fetching validators data from beacon
func (mm *MetadataManager) fetchValidatorsMetadata(validatorPubKeys []spectypes.ValidatorPK) (map[phase0.BLSPubKey]*beaconprotocol.ValidatorMetadata, error) {
	if len(validatorPubKeys) == 0 {
		return nil, nil
	}
	var blsPubKeys []phase0.BLSPubKey
	for _, validatorPubKey := range validatorPubKeys {
		blsPubKey := phase0.BLSPubKey{}
		copy(blsPubKey[:], validatorPubKey)
		blsPubKeys = append(blsPubKeys, blsPubKey)
	}
	validatorsIndexMap, err := mm.beaconNode.GetValidatorData(blsPubKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to get validators data from beacon: %w", err)
	}
	ret := make(map[phase0.BLSPubKey]*beaconprotocol.ValidatorMetadata)
	for _, v := range validatorsIndexMap {
		meta := &beaconprotocol.ValidatorMetadata{
			Balance:         v.Balance,
			Status:          v.Status,
			Index:           v.Index,
			ActivationEpoch: v.Validator.ActivationEpoch,
		}
		ret[v.Validator.PublicKey] = meta
	}
	return ret, nil
}

func (mm *MetadataManager) byNotRecentlyUpdated(s *ssvtypes.SSVShare) bool {
	last, ok := mm.metadataLastUpdated[string(s.ValidatorPubKey)]
	return !ok || time.Since(last) > mm.metadataUpdateInterval
}
