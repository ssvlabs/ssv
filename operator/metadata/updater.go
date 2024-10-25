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
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

//go:generate mockgen -package=mocks -destination=./mocks/mock_storage.go -source=./updater.go

const (
	defaultUpdateInterval = 12 * time.Minute
	defaultStreamInterval = 2 * time.Second
	defaultBatchSize      = 512
)

type Updater struct {
	logger         *zap.Logger
	shareStorage   shareStorage
	beaconNetwork  beacon.BeaconNetwork
	fetcher        *Fetcher
	updateInterval time.Duration
	streamInterval time.Duration
	batchSize      int
}

type shareStorage interface {
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	Range(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool)
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata) error
}

func NewUpdater(
	logger *zap.Logger,
	sharesStorage shareStorage,
	beaconNetwork beacon.BeaconNetwork,
	beaconNode beacon.BeaconNode,
	opts ...Option,
) *Updater {
	u := &Updater{
		logger:         logger,
		shareStorage:   sharesStorage,
		beaconNetwork:  beaconNetwork,
		fetcher:        NewFetcher(logger, beaconNode),
		updateInterval: defaultUpdateInterval,
		streamInterval: defaultStreamInterval,
	}

	for _, opt := range opts {
		opt(u)
	}

	return u
}

type Option func(*Updater)

func WithUpdateInterval(interval time.Duration) Option {
	return func(u *Updater) {
		u.updateInterval = interval
	}
}

func WithStreamInterval(interval time.Duration) Option {
	return func(u *Updater) {
		u.updateInterval = interval
	}
}

func WithBatchSize(batchSize int) Option {
	return func(u *Updater) {
		u.batchSize = batchSize
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

	// Fetch metadata now if there is none. Otherwise, Stream will handle it.
	return u.Update(ctx, allPubKeys)
}

type Validators = map[spectypes.ValidatorPK]*beacon.ValidatorMetadata

func (u *Updater) Update(ctx context.Context, pubKeys []spectypes.ValidatorPK) (Validators, error) {
	fetchStart := time.Now()
	metadata, err := u.fetcher.Fetch(ctx, pubKeys)
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

type Update struct {
	IndicesBefore []phase0.ValidatorIndex
	IndicesAfter  []phase0.ValidatorIndex
	Validators    Validators
}

func (u *Updater) Stream(ctx context.Context) <-chan Update {
	metadataUpdates := make(chan Update)

	go func() {
		defer close(metadataUpdates)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				shares := u.sharesForUpdate()

				if len(shares) > 0 {
					pubKeys := make([]spectypes.ValidatorPK, len(shares))
					for i, s := range shares {
						pubKeys[i] = s.ValidatorPubKey
					}

					beforeUpdate := u.allActiveIndices(u.beaconNetwork.GetBeaconNetwork().EstimatedCurrentEpoch())

					updatedMetadata, err := u.Update(ctx, pubKeys)
					if err != nil {
						u.logger.Warn("failed to update validators metadata",
							zap.Int("shares", len(shares)),
							zap.Error(err))
						continue
					}

					// Refresh duties if there are any new active validators.
					afterUpdate := u.allActiveIndices(u.beaconNetwork.GetBeaconNetwork().EstimatedCurrentEpoch())

					metadataUpdates <- Update{
						IndicesBefore: beforeUpdate,
						IndicesAfter:  afterUpdate,
						Validators:    updatedMetadata,
					}
				}

				// Only sleep if there aren't more validators to fetch metadata for.
				if len(shares) < u.batchSize {
					time.Sleep(u.streamInterval)
				}
			}
		}
	}()

	return metadataUpdates
}

func (u *Updater) sharesForUpdate() []*ssvtypes.SSVShare {
	var existingShares, newShares []*ssvtypes.SSVShare
	u.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.Liquidated {
			return true
		}
		if share.BeaconMetadata == nil && share.MetadataLastUpdated().IsZero() {
			newShares = append(newShares, share)
		} else if time.Since(share.MetadataLastUpdated()) > u.updateInterval {
			existingShares = append(existingShares, share)
		}
		return len(newShares) < u.batchSize
	})

	// Combine validators up to batchSize, prioritizing the new ones.
	shares := newShares
	if remainder := u.batchSize - len(shares); remainder > 0 {
		end := remainder
		if end > len(existingShares) {
			end = len(existingShares)
		}
		shares = append(shares, existingShares[:end]...)
	}

	for _, share := range shares {
		share.SetMetadataLastUpdated(time.Now())
	}
	return shares
}

// TODO: Create a wrapper for share storage that contains all common methods like AllActiveIndices and use the wrapper.
func (u *Updater) allActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex {
	var indices []phase0.ValidatorIndex

	u.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.IsParticipating(epoch) {
			indices = append(indices, share.BeaconMetadata.Index)
		}
		return true
	})

	return indices
}
