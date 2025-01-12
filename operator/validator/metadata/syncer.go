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

//go:generate mockgen -package=metadata -destination=./mocks.go -source=./syncer.go

const (
	defaultSyncInterval      = 12 * time.Minute
	defaultStreamInterval    = 2 * time.Second
	defaultUpdateSendTimeout = 30 * time.Second
	batchSize                = 512
)

type Syncer struct {
	logger            *zap.Logger
	shareStorage      shareStorage
	beaconNetwork     beacon.BeaconNetwork
	beaconNode        beacon.BeaconNode
	syncInterval      time.Duration
	streamInterval    time.Duration
	updateSendTimeout time.Duration
}

type shareStorage interface {
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	Range(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool)
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata) error
}

func NewSyncer(
	logger *zap.Logger,
	shareStorage shareStorage,
	beaconNetwork beacon.BeaconNetwork,
	beaconNode beacon.BeaconNode,
	opts ...Option,
) *Syncer {
	u := &Syncer{
		logger:            logger,
		shareStorage:      shareStorage,
		beaconNetwork:     beaconNetwork,
		beaconNode:        beaconNode,
		syncInterval:      defaultSyncInterval,
		streamInterval:    defaultStreamInterval,
		updateSendTimeout: defaultUpdateSendTimeout,
	}

	for _, opt := range opts {
		opt(u)
	}

	return u
}

type Option func(*Syncer)

func WithSyncInterval(interval time.Duration) Option {
	return func(u *Syncer) {
		u.syncInterval = interval
	}
}

func (s *Syncer) SyncOnStartup(ctx context.Context) (map[spectypes.ValidatorPK]*beacon.ValidatorMetadata, error) {
	// Load non-liquidated shares.
	shares := s.shareStorage.List(nil, registrystorage.ByNotLiquidated())
	if len(shares) == 0 {
		s.logger.Info("could not find non-liquidated validator shares on initial metadata retrieval")
		return nil, nil
	}

	// Skip syncing if metadata was already fetched before
	// to prevent blocking startup after first sync.
	needToSync := false
	allPubKeys := make([]spectypes.ValidatorPK, 0, len(shares))
	for _, share := range shares {
		allPubKeys = append(allPubKeys, share.ValidatorPubKey)
		if !share.HasBeaconMetadata() {
			needToSync = true
		}
	}
	if !needToSync {
		// Stream should take it over from here.
		return nil, nil
	}

	// Sync all pubkeys. We don't need to batch them because we need to wait here until all of them are synced.
	return s.Sync(ctx, allPubKeys)
}

type SyncBatch struct {
	IndicesBefore []phase0.ValidatorIndex
	IndicesAfter  []phase0.ValidatorIndex
	Validators    ValidatorMap
}

type ValidatorMap = map[spectypes.ValidatorPK]*beacon.ValidatorMetadata

func (s *Syncer) Sync(ctx context.Context, pubKeys []spectypes.ValidatorPK) (ValidatorMap, error) {
	fetchStart := time.Now()
	metadata, err := s.Fetch(ctx, pubKeys)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}

	s.logger.Debug("🆕 fetched metadata for validator shares",
		fields.Took(time.Since(fetchStart)),
		zap.Int("metadata_cnt", len(metadata)),
		zap.Int("shares_cnt", len(pubKeys)),
	)

	updateStart := time.Now()
	// TODO: Refactor share storage to support passing context.
	if err := s.shareStorage.UpdateValidatorsMetadata(metadata); err != nil {
		return metadata, fmt.Errorf("update metadata: %w", err)
	}

	s.logger.Debug("🆕 updated validators metadata in storage",
		fields.Took(time.Since(updateStart)),
		zap.Int("metadata_count", len(metadata)),
		zap.Int("shares_cnt", len(pubKeys)),
	)

	return metadata, nil
}

func (s *Syncer) Fetch(_ context.Context, pubKeys []spectypes.ValidatorPK) (ValidatorMap, error) {
	if len(pubKeys) == 0 {
		return nil, nil
	}

	var blsPubKeys []phase0.BLSPubKey
	for _, pk := range pubKeys {
		blsPubKeys = append(blsPubKeys, phase0.BLSPubKey(pk))
	}

	// TODO: Refactor beacon.BeaconNode to support passing context.
	validatorsIndexMap, err := s.beaconNode.GetValidatorData(blsPubKeys)
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

func (s *Syncer) Stream(ctx context.Context) <-chan SyncBatch {
	metadataUpdates := make(chan SyncBatch)

	go func() {
		defer close(metadataUpdates)

		for {
			batch, done, err := s.syncNextBatch(ctx)
			if err != nil {
				s.logger.Warn("failed to prepare validators metadata",
					zap.Error(err),
				)
				if slept := s.sleep(ctx, s.streamInterval); !slept {
					return
				}
				continue
			}

			if len(batch.Validators) == 0 {
				if slept := s.sleep(ctx, s.streamInterval); !slept {
					return
				}
				continue
			}

			// TODO: use time.After when Go is updated to 1.23
			timer := time.NewTimer(s.updateSendTimeout)
			select {
			case metadataUpdates <- batch:
				// Only sleep for the last batch.
				if done {
					if slept := s.sleep(ctx, s.streamInterval); !slept {
						timer.Stop()
						return
					}
				}
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.logger.Warn("timed out waiting for sending update")
			}
			timer.Stop()
		}
	}()

	return metadataUpdates
}

// syncNextBatch syncs the next batch.
// It is used only by Stream method.
// The maximal size is batchSize as we want to reduce the load while streaming.
// Therefore, syncNextBatch should be called in a loop, so the rest will be prepared by next calls.
func (s *Syncer) syncNextBatch(ctx context.Context) (SyncBatch, bool, error) {
	// TODO: Methods called here don't handle context, so this is a workaround to handle done context. It should be removed once ctx is handled gracefully.
	select {
	case <-ctx.Done():
		return SyncBatch{}, false, ctx.Err()
	default:
	}

	shares := s.nextBatch(ctx)
	if len(shares) == 0 {
		return SyncBatch{}, false, nil
	}

	pubKeys := make([]spectypes.ValidatorPK, len(shares))
	for i, share := range shares {
		pubKeys[i] = share.ValidatorPubKey
	}

	indicesBefore := s.allActiveIndices(ctx, s.beaconNetwork.GetBeaconNetwork().EstimatedCurrentEpoch())

	validators, err := s.Sync(ctx, pubKeys)
	if err != nil {
		return SyncBatch{}, false, fmt.Errorf("sync: %w", err)
	}

	indicesAfter := s.allActiveIndices(ctx, s.beaconNetwork.GetBeaconNetwork().EstimatedCurrentEpoch())

	update := SyncBatch{
		IndicesBefore: indicesBefore,
		IndicesAfter:  indicesAfter,
		Validators:    validators,
	}

	return update, len(shares) < batchSize, nil
}

// nextBatch returns non-liquidated shares from DB that are most deserving of an update, it relies on share.Metadata.lastUpdated to be updated in order to keep iterating forward.
func (s *Syncer) nextBatch(_ context.Context) []*ssvtypes.SSVShare {
	// TODO: use context, return if it's done
	var staleShares, newShares []*ssvtypes.SSVShare
	s.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.Liquidated {
			return true
		}

		shareMetadataStaleAfter := s.syncInterval
		// also check last metadata update to differentiate new shares from stale ones
		if !share.HasBeaconMetadata() && share.MetadataLastUpdated().IsZero() {
			newShares = append(newShares, share)
		} else if time.Since(share.MetadataLastUpdated()) > shareMetadataStaleAfter {
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
func (s *Syncer) allActiveIndices(_ context.Context, epoch phase0.Epoch) []phase0.ValidatorIndex {
	var indices []phase0.ValidatorIndex

	// TODO: use context, return if it's done
	s.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.IsParticipating(epoch) {
			indices = append(indices, share.BeaconMetadata.Index)
		}
		return true
	})

	return indices
}

func (s *Syncer) sleep(ctx context.Context, d time.Duration) (slept bool) {
	// TODO: use time.After when Go is updated to 1.23
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
