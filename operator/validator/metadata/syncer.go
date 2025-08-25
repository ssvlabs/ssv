package metadata

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	networkcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

//go:generate go tool -modfile=../../../tool.mod mockgen -package=metadata -destination=./mocks.go -source=./syncer.go

const (
	defaultSyncInterval      = 12 * time.Minute
	defaultStreamInterval    = 2 * time.Second
	defaultUpdateSendTimeout = 30 * time.Second
	// NOTE:
	// A higher value of `batchSize` results in fewer HTTP calls to the Consensus Node,
	// but each call will have larger payloads and responses. While this speeds up
	// metadata synchronization, it also increases the risk of timeouts.
	//
	// The value of `batchSize` should depend on the number of validators the node handles
	// (especially relevant when comparing Exporter vs. Non-Exporter nodes)
	// and the sync interval (how often metadata should be refreshed).
	//
	// ⚠️ Caution: Since there is no prioritization implemented, if the node cannot
	// sync all validator shares within the given sync interval, there is a high risk
	// that some validators will not be refreshed for an unpredictable amount of time.
	batchSize = 512
)

type Syncer struct {
	logger            *zap.Logger
	shareStorage      shareStorage
	validatorStore    selfValidatorStore
	beaconNode        beacon.BeaconNode
	fixedSubnets      networkcommons.Subnets
	syncInterval      time.Duration
	streamInterval    time.Duration
	updateSendTimeout time.Duration
}

type shareStorage interface {
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	Range(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool)
	UpdateValidatorsMetadata(beacon.ValidatorMetadataMap) (beacon.ValidatorMetadataMap, error)
}

type selfValidatorStore interface {
	SelfValidators() []*ssvtypes.SSVShare
}

func NewSyncer(
	logger *zap.Logger,
	shareStorage shareStorage,
	validatorStore selfValidatorStore,
	beaconNode beacon.BeaconNode,
	fixedSubnets networkcommons.Subnets,
	opts ...Option,
) *Syncer {
	u := &Syncer{
		logger:            logger.Named(log.NameShareMetadataSyncer),
		shareStorage:      shareStorage,
		validatorStore:    validatorStore,
		beaconNode:        beaconNode,
		fixedSubnets:      fixedSubnets,
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

// SyncAll loads all non-liquidated validator shares that belong to operator's subnets,
// and triggers a full metadata synchronization for them.
// It returns a mapping of validator public keys to their updated metadata.
func (s *Syncer) SyncAll(ctx context.Context) (beacon.ValidatorMetadataMap, error) {
	subnetsBuf := new(big.Int)
	ownSubnets := s.selfSubnets(subnetsBuf)

	// Load non-liquidated shares.
	shares := s.shareStorage.List(nil, registrystorage.ByNotLiquidated(), func(share *ssvtypes.SSVShare) bool {
		networkcommons.SetCommitteeSubnet(subnetsBuf, share.CommitteeID())
		subnet := subnetsBuf.Uint64()
		return ownSubnets.IsSet(subnet)
	})
	if len(shares) == 0 {
		s.logger.Info("could not find non-liquidated own subnets validator shares on initial metadata retrieval")
		return nil, nil
	}

	pubKeysToFetch := make([]spectypes.ValidatorPK, 0, len(shares))
	for _, share := range shares {
		pubKeysToFetch = append(pubKeysToFetch, share.ValidatorPubKey)
	}

	// Sync all pubkeys that belong to own subnets. We don't need to batch them because we need to wait here until all of them are synced.
	return s.Sync(ctx, pubKeysToFetch)
}

// Sync retrieves metadata for the provided public keys and updates storage accordingly.
// Returns updated metadata for keys that had changes. Returns nil if no keys were provided or no updates occurred.
func (s *Syncer) Sync(ctx context.Context, pubKeys []spectypes.ValidatorPK) (beacon.ValidatorMetadataMap, error) {
	if len(pubKeys) == 0 {
		return nil, nil
	}

	fetchStart := time.Now()
	metadata, err := s.fetchMetadata(ctx, pubKeys)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}

	s.logger.Debug("🆕 fetched validators metadata",
		fields.Took(time.Since(fetchStart)),
		zap.Int("requested_count", len(pubKeys)),
		zap.Int("received_count", len(metadata)),
	)

	updateStart := time.Now()
	// TODO: Refactor share storage to support passing context.
	updatedValidators, err := s.shareStorage.UpdateValidatorsMetadata(metadata)
	if err != nil {
		return nil, fmt.Errorf("update metadata: %w", err)
	}

	s.logger.Debug("🆕 saved validators metadata",
		fields.Took(time.Since(updateStart)),
		zap.Int("received_count", len(metadata)),
		zap.Int("updated_count", len(updatedValidators)),
	)

	return updatedValidators, nil
}

// fetchMetadata is responsible for fetching validator metadata from the beacon node for the provided public keys.
// The beacon node response is sometimes empty for certain public keys — for such validators,
// the ValidatorMetadataMap will contain empty metadata objects.
func (s *Syncer) fetchMetadata(ctx context.Context, pubKeys []spectypes.ValidatorPK) (beacon.ValidatorMetadataMap, error) {
	blsPubKeys := make([]phase0.BLSPubKey, len(pubKeys))
	for i, pk := range pubKeys {
		blsPubKeys[i] = phase0.BLSPubKey(pk)
	}

	validatorsIndexMap, err := s.beaconNode.GetValidatorData(ctx, blsPubKeys)
	if err != nil {
		return nil, fmt.Errorf("get validator data from beacon node: %w", err)
	}

	results := make(beacon.ValidatorMetadataMap, len(pubKeys))

	for _, key := range pubKeys {
		results[key] = &beacon.ValidatorMetadata{}
	}

	for _, v := range validatorsIndexMap {
		meta := &beacon.ValidatorMetadata{
			Status:          v.Status,
			Index:           v.Index,
			ActivationEpoch: v.Validator.ActivationEpoch,
			ExitEpoch:       v.Validator.ExitEpoch,
		}
		results[spectypes.ValidatorPK(v.Validator.PublicKey)] = meta
	}

	return results, nil
}

// Stream continuously fetches and streams batches of validator metadata updates as they become available.
// It yields updates through a channel (`SyncBatch`) and handles retries, sleeping between sync attempts
// when all metadata is up to date. The loop respects the provided context and stops gracefully when canceled.
func (s *Syncer) Stream(ctx context.Context) <-chan SyncBatch {
	metadataUpdates := make(chan SyncBatch)

	go func() {
		defer close(metadataUpdates)

		subnetsBuf := new(big.Int)

		for {
			batch, done, err := s.syncNextBatch(ctx, subnetsBuf)
			if err != nil {
				s.logger.Warn("failed to prepare validators metadata",
					zap.Error(err),
				)
				if slept := s.sleep(ctx, s.streamInterval); !slept {
					return
				}
				continue
			}

			if len(batch.Before) == 0 {
				s.logger.Debug("sleeping because all validators’ metadata has been refreshed.",
					zap.Duration("sleep_for", s.streamInterval),
					zap.Duration("refresh_interval", s.syncInterval))
				if slept := s.sleep(ctx, s.streamInterval); !slept {
					return
				}
				continue
			}

			select {
			case metadataUpdates <- batch:
				// Only sleep if there aren't more validators to fetch metadata for.
				// It's done to wait for some data to appear. Without sleep, the next batch would likely be empty.
				if done {
					s.logger.Debug("sleeping after batch was streamed because all validators’ metadata has been refreshed.",
						zap.Duration("sleep_for", s.streamInterval),
						zap.Duration("refresh_interval", s.syncInterval))
					if slept := s.sleep(ctx, s.streamInterval); !slept {
						return
					}
				}
			case <-ctx.Done():
				return
			case <-time.After(s.updateSendTimeout):
				s.logger.Warn("timed out waiting for sending update")
			}
		}
	}()

	return metadataUpdates
}

// syncNextBatch syncs the next batch.
// It is used only by Stream method.
// The maximal size is batchSize as we want to reduce the load while streaming.
// Therefore, syncNextBatch should be called in a loop, so the rest will be prepared by next calls.
func (s *Syncer) syncNextBatch(ctx context.Context, subnetsBuf *big.Int) (SyncBatch, bool, error) {
	// TODO: Methods called here don't handle context, so this is a workaround to handle done context. It should be removed once ctx is handled gracefully.
	select {
	case <-ctx.Done():
		return SyncBatch{}, false, ctx.Err()
	default:
	}

	beforeMetadata := s.nextBatchFromDB(ctx, subnetsBuf)
	if len(beforeMetadata) == 0 {
		return SyncBatch{}, false, nil
	}

	afterMetadata, err := s.Sync(ctx, slices.Collect(maps.Keys(beforeMetadata)))
	if err != nil {
		return SyncBatch{}, false, fmt.Errorf("sync: %w", err)
	}

	syncBatch := SyncBatch{
		Before: beforeMetadata,
		After:  afterMetadata,
	}

	return syncBatch, len(beforeMetadata) < batchSize, nil
}

// nextBatchFromDB returns metadata for non-liquidated shares from DB that are most deserving of an update.
// It prioritizes shares whose metadata was never fetched or became stale based on BeaconMetadataLastUpdated.
func (s *Syncer) nextBatchFromDB(_ context.Context, subnetsBuf *big.Int) beacon.ValidatorMetadataMap {
	// TODO: use context, return if it's done
	ownSubnets := s.selfSubnets(subnetsBuf)

	var staleShares, newShares []*ssvtypes.SSVShare
	s.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.Liquidated {
			return true
		}

		networkcommons.SetCommitteeSubnet(subnetsBuf, share.CommitteeID())
		subnet := subnetsBuf.Uint64()
		if !ownSubnets.IsSet(subnet) {
			return true
		}

		// Fetch new and stale shares only.
		if !share.HasBeaconMetadata() && share.BeaconMetadataLastUpdated.IsZero() {
			// Metadata was never fetched for this share, so it's considered new.
			newShares = append(newShares, share)
		} else if time.Since(share.BeaconMetadataLastUpdated) > s.syncInterval {
			// Metadata hasn't been fetched for a while, so it's considered stale.
			staleShares = append(staleShares, share)
		}

		return len(newShares)+len(staleShares) < batchSize
	})

	// Combine validators up to batchSize, prioritizing the new ones.
	shares := append(newShares, staleShares...)
	if len(shares) > batchSize {
		shares = shares[:batchSize]
	}

	metadataMap := make(beacon.ValidatorMetadataMap, len(shares))
	for _, share := range shares {
		metadataMap[share.ValidatorPubKey] = share.BeaconMetadata()
	}

	return metadataMap
}

func (s *Syncer) sleep(ctx context.Context, d time.Duration) (slept bool) {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// selfSubnets calculates the operator's subnets by adding up the fixed subnets and the active committees
// it recvs big int buffer for memory reusing, if is nil it will allocate new
func (s *Syncer) selfSubnets(buf *big.Int) networkcommons.Subnets {
	// Start off with a copy of the fixed subnets (e.g., exporter subscribed to all subnets).
	localBuf := buf
	if localBuf == nil {
		localBuf = new(big.Int)
	}

	mySubnets := s.fixedSubnets

	// Compute the new subnets according to the active committees/validators.
	myValidators := s.validatorStore.SelfValidators()
	for _, v := range myValidators {
		networkcommons.SetCommitteeSubnet(localBuf, v.CommitteeID())
		mySubnets.Set(localBuf.Uint64())
	}

	return mySubnets
}
