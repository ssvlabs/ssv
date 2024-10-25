package metadata

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

type Fetcher struct {
	logger     *zap.Logger
	beaconNode beacon.BeaconNode
}

func NewFetcher(logger *zap.Logger, beaconNode beacon.BeaconNode) *Fetcher {
	return &Fetcher{
		logger:     logger,
		beaconNode: beaconNode,
	}
}

func (mf *Fetcher) Fetch(_ context.Context, pubKeys []spectypes.ValidatorPK) (map[spectypes.ValidatorPK]*beacon.ValidatorMetadata, error) {
	if len(pubKeys) == 0 {
		return nil, nil
	}

	var blsPubKeys []phase0.BLSPubKey
	for _, pk := range pubKeys {
		blsPubKeys = append(blsPubKeys, phase0.BLSPubKey(pk))
	}

	start := time.Now()
	// TODO: Refactor beacon.BeaconNode to support passing context.
	validatorsIndexMap, err := mf.beaconNode.GetValidatorData(blsPubKeys)
	if err != nil {
		mf.logger.Error("failed to fetch initial validators metadata",
			zap.Int("shares", len(pubKeys)),
			fields.Took(time.Since(start)),
			zap.Error(err),
		)
		return nil, fmt.Errorf("get validator data from beacon node: %w", err)
	}

	results := make(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata)
	for _, v := range validatorsIndexMap {
		meta := &beacon.ValidatorMetadata{
			Balance:         v.Balance,
			Status:          v.Status,
			Index:           v.Index,
			ActivationEpoch: v.Validator.ActivationEpoch,
		}
		results[spectypes.ValidatorPK(v.Validator.PublicKey)] = meta
	}

	mf.logger.Debug("⏱️ fetched validators metadata",
		zap.Duration("elapsed", time.Since(start)),
		zap.Int("requested", len(pubKeys)),
		zap.Int("received", len(results)),
	)

	return results, nil
}
