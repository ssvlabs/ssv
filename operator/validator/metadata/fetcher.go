package metadata

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

type Fetcher struct {
	logger     *zap.Logger
	beaconNode beacon.BeaconNode
}

type ValidatorMap = map[spectypes.ValidatorPK]*beacon.ValidatorMetadata

func NewFetcher(logger *zap.Logger, beaconNode beacon.BeaconNode) *Fetcher {
	return &Fetcher{
		logger:     logger,
		beaconNode: beaconNode,
	}
}

func (mf *Fetcher) Fetch(_ context.Context, pubKeys []spectypes.ValidatorPK) (ValidatorMap, error) {
	if len(pubKeys) == 0 {
		return nil, nil
	}

	var blsPubKeys []phase0.BLSPubKey
	for _, pk := range pubKeys {
		blsPubKeys = append(blsPubKeys, phase0.BLSPubKey(pk))
	}

	// TODO: Refactor beacon.BeaconNode to support passing context.
	validatorsIndexMap, err := mf.beaconNode.GetValidatorData(blsPubKeys)
	if err != nil {
		return nil, fmt.Errorf("get validator data from beacon node: %w", err)
	}

	results := make(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata, len(validatorsIndexMap))
	for _, v := range validatorsIndexMap {
		meta := &beacon.ValidatorMetadata{
			Balance:         v.Balance,
			Status:          v.Status,
			Index:           v.Index,
			ActivationEpoch: v.Validator.ActivationEpoch,
		}
		results[spectypes.ValidatorPK(v.Validator.PublicKey)] = meta
	}

	return results, nil
}
