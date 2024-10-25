package metadata

import (
	"context"
	"fmt"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

//go:generate mockgen -package=mocks -destination=./mocks/mock_storage.go -source=./updater.go

type Updater struct {
	logger          *zap.Logger
	shareStorage    shareStorage
	metadataFetcher *Fetcher
}

type shareStorage interface {
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata) error
}

func NewUpdater(logger *zap.Logger, sharesStorage shareStorage, beaconNode beacon.BeaconNode) *Updater {
	return &Updater{
		logger:          logger,
		shareStorage:    sharesStorage,
		metadataFetcher: NewFetcher(logger, beaconNode),
	}
}

func (u *Updater) RetrieveInitialMetadata(ctx context.Context) (map[spectypes.ValidatorPK]*beacon.ValidatorMetadata, error) {
	// Load non-liquidated shares.
	shares := u.shareStorage.List(nil, registrystorage.ByNotLiquidated())
	if len(shares) == 0 {
		u.logger.Info("could not find validators")
		return nil, nil
	}

	var hasMetadata bool
	allPubKeys := make([]spectypes.ValidatorPK, 0, len(shares))
	for _, share := range shares {
		allPubKeys = append(allPubKeys, share.ValidatorPubKey)
		if !share.Liquidated && share.HasBeaconMetadata() {
			hasMetadata = true // TODO: the behavior is preserved; should it be true if all shares have metadata?
			break
		}
	}

	if hasMetadata {
		return nil, nil
	}

	// Fetch metadata now if there is none. Otherwise, UpdateValidatorMetaDataLoop in validator controller will handle it.
	return u.Update(ctx, allPubKeys)
}

func (u *Updater) Update(ctx context.Context, pubKeys []spectypes.ValidatorPK) (map[spectypes.ValidatorPK]*beacon.ValidatorMetadata, error) {
	fetchStart := time.Now()
	metadata, err := u.metadataFetcher.Fetch(ctx, pubKeys)
	if err != nil {
		u.logger.Error("failed to fetch initial validators metadata",
			zap.Int("shares", len(pubKeys)),
			fields.Took(time.Since(fetchStart)),
			zap.Error(err),
		) // TODO: is returning error enough?
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}

	u.logger.Debug("🆕 fetched metadata",
		fields.Took(time.Since(fetchStart)),
		zap.Int("count", len(metadata)),
		zap.Int("shares", len(pubKeys)),
	)

	updateStart := time.Now()
	// TODO: Refactor share storage to support passing context.
	if err := u.shareStorage.UpdateValidatorsMetadata(metadata); err != nil {
		u.logger.Error("failed to update validators metadata after setup",
			zap.Int("shares", len(pubKeys)),
			fields.Took(time.Since(updateStart)),
			zap.Error(err),
		) // TODO: is returning error enough?
		return metadata, fmt.Errorf("update metadata: %w", err)
	}

	u.logger.Debug("🆕 updated validators metadata in storage",
		fields.Took(time.Since(updateStart)),
		zap.Int("count", len(metadata)),
		zap.Int("shares", len(pubKeys)),
	)
	return metadata, nil
}
