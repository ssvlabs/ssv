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

//go:generate mockgen -package=metadata -destination=./mocks.go -source=./updater.go

const (
	defaultUpdateInterval    = 12 * time.Minute
	defaultStreamInterval    = 2 * time.Second
	defaultUpdateSendTimeout = 30 * time.Second
	batchSize                = 512
)

type Updater struct {
	logger            *zap.Logger
	shareStorage      shareStorage
	beaconNetwork     beacon.BeaconNetwork
	fetcher           fetcher
	updateInterval    time.Duration
	streamInterval    time.Duration
	updateSendTimeout time.Duration
}

type shareStorage interface {
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	Range(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool)
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata) error
}

type fetcher interface {
	Fetch(ctx context.Context, pubKeys []spectypes.ValidatorPK) (Validators, error)
}

func NewUpdater(
	logger *zap.Logger,
	shareStorage shareStorage,
	beaconNetwork beacon.BeaconNetwork,
	beaconNode beacon.BeaconNode,
	opts ...Option,
) *Updater {
	u := &Updater{
		logger:            logger,
		shareStorage:      shareStorage,
		beaconNetwork:     beaconNetwork,
		fetcher:           NewFetcher(logger, beaconNode),
		updateInterval:    defaultUpdateInterval,
		streamInterval:    defaultStreamInterval,
		updateSendTimeout: defaultUpdateSendTimeout,
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

func (u *Updater) UpdateOnStartup(ctx context.Context) (map[spectypes.ValidatorPK]*beacon.ValidatorMetadata, error) {
	// Load non-liquidated shares.
	shares := u.shareStorage.List(nil, registrystorage.ByNotLiquidated())
	if len(shares) == 0 {
		u.logger.Info("could not find non-liquidated validator shares on initial metadata retrieval")
		return nil, nil
	}

	needToUpdate := false
	allPubKeys := make([]spectypes.ValidatorPK, 0, len(shares))
	for _, share := range shares {
		allPubKeys = append(allPubKeys, share.ValidatorPubKey)
		if !share.HasBeaconMetadata() {
			needToUpdate = true
		}
	}

	if !needToUpdate {
		// No need to fetch metadata if all shares have it. It's going to be updated by Stream method afterwards.
		return nil, nil
	}

	return u.Update(ctx, allPubKeys)
}

func (u *Updater) Update(ctx context.Context, pubKeys []spectypes.ValidatorPK) (Validators, error) {
	fetchStart := time.Now()
	metadata, err := u.fetcher.Fetch(ctx, pubKeys)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}

	u.logger.Debug("🆕 fetched metadata for validator shares",
		fields.Took(time.Since(fetchStart)),
		zap.Int("metadata_cnt", len(metadata)),
		zap.Int("shares_cnt", len(pubKeys)),
	)

	updateStart := time.Now()
	// TODO: Refactor share storage to support passing context.
	if err := u.shareStorage.UpdateValidatorsMetadata(metadata); err != nil {
		return metadata, fmt.Errorf("update metadata: %w", err)
	}

	u.logger.Debug("🆕 updated validators metadata in storage",
		fields.Took(time.Since(updateStart)),
		zap.Int("metadata_count", len(metadata)),
		zap.Int("shares_cnt", len(pubKeys)),
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
			update, done, err := u.prepareUpdate(ctx)
			if err != nil {
				u.logger.Warn("failed to prepare validators metadata",
					zap.Error(err),
				)
				if slept := u.sleep(ctx, u.streamInterval); !slept {
					return
				}
				continue
			}

			if len(update.Validators) == 0 {
				continue
			}

			timer := time.NewTimer(u.updateSendTimeout)
			select {
			case metadataUpdates <- update:
				// Only sleep for the last batch.
				if done {
					if slept := u.sleep(ctx, u.streamInterval); !slept {
						timer.Stop()
						return
					}
				}
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				u.logger.Warn("timed out waiting for sending update")
			}
			timer.Stop()
		}
	}()

	return metadataUpdates
}

func (u *Updater) prepareUpdate(ctx context.Context) (Update, bool, error) {
	// TODO: Methods called here don't handle context, so this is a workaround to handle done context. It should be removed once ctx is handled gracefully.
	select {
	case <-ctx.Done():
		return Update{}, false, ctx.Err()
	default:
	}

	shares := u.sharesForUpdate(ctx)
	if len(shares) == 0 {
		return Update{}, false, nil
	}

	pubKeys := make([]spectypes.ValidatorPK, len(shares))
	for i, s := range shares {
		pubKeys[i] = s.ValidatorPubKey
	}

	indicesBefore := u.allActiveIndices(ctx, u.beaconNetwork.GetBeaconNetwork().EstimatedCurrentEpoch())

	updatedMetadata, err := u.Update(ctx, pubKeys)
	if err != nil {
		return Update{}, false, fmt.Errorf("update metadata: %w", err)
	}

	indicesAfter := u.allActiveIndices(ctx, u.beaconNetwork.GetBeaconNetwork().EstimatedCurrentEpoch())

	update := Update{
		IndicesBefore: indicesBefore,
		IndicesAfter:  indicesAfter,
		Validators:    updatedMetadata,
	}

	return update, len(shares) < batchSize, nil
}

// sharesForUpdate returns non-liquidated shares from DB that are most deserving of an update, it relies on share.Metadata.lastUpdated to be updated in order to keep iterating forward.
func (u *Updater) sharesForUpdate(_ context.Context) []*ssvtypes.SSVShare {
	// TODO: use context, return if it's done
	var staleShares, newShares []*ssvtypes.SSVShare
	u.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.Liquidated {
			return true
		}

		if share.BeaconMetadata == nil && share.MetadataLastUpdated().IsZero() {
			newShares = append(newShares, share)
		} else if time.Since(share.MetadataLastUpdated()) > u.updateInterval {
			staleShares = append(staleShares, share)
		}
		return len(newShares) < batchSize
	})

	// Combine validators up to batchSize, prioritizing the new ones.
	shares := newShares
	if remainder := batchSize - len(shares); remainder > 0 {
		end := remainder
		if end > len(staleShares) {
			end = len(staleShares)
		}
		shares = append(shares, staleShares[:end]...)
	}

	for _, share := range shares {
		share.SetMetadataLastUpdated(time.Now())
	}
	return shares
}

// TODO: Create a wrapper for share storage that contains all common methods like AllActiveIndices and use the wrapper.
func (u *Updater) allActiveIndices(_ context.Context, epoch phase0.Epoch) []phase0.ValidatorIndex {
	var indices []phase0.ValidatorIndex

	// TODO: use context, return if it's done
	u.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.IsParticipating(epoch) {
			indices = append(indices, share.BeaconMetadata.Index)
		}
		return true
	})

	return indices
}

func (u *Updater) sleep(ctx context.Context, d time.Duration) (slept bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
