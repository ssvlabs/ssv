package beacon

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

type MetadataFetcher struct {
	logger     *zap.Logger
	beaconNode BeaconNode
}

func NewMetadataFetcher(logger *zap.Logger, beaconNode BeaconNode) *MetadataFetcher {
	return &MetadataFetcher{
		logger:     logger,
		beaconNode: beaconNode,
	}
}

func (mf *MetadataFetcher) Fetch(pubKeys []spectypes.ValidatorPK) (map[spectypes.ValidatorPK]*ValidatorMetadata, error) {
	if len(pubKeys) == 0 {
		return nil, nil
	}

	var blsPubKeys []phase0.BLSPubKey
	for _, pk := range pubKeys {
		blsPubKeys = append(blsPubKeys, phase0.BLSPubKey(pk))
	}

	start := time.Now()
	validatorsIndexMap, err := mf.beaconNode.GetValidatorData(blsPubKeys)
	if err != nil {
		return nil, fmt.Errorf("get validator data from beacon node: %w", err)
	}
	elapsed := time.Since(start)

	results := make(map[spectypes.ValidatorPK]*ValidatorMetadata)
	for _, v := range validatorsIndexMap {
		meta := &ValidatorMetadata{
			Balance:         v.Balance,
			Status:          v.Status,
			Index:           v.Index,
			ActivationEpoch: v.Validator.ActivationEpoch,
		}
		results[spectypes.ValidatorPK(v.Validator.PublicKey)] = meta
	}

	mf.logger.Debug("⏱️ fetched validators metadata",
		zap.Duration("elapsed", elapsed),
		zap.Int("requested", len(pubKeys)),
		zap.Int("received", len(results)),
	)

	return results, nil
}
