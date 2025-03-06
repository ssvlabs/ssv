package metadata

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	networkcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
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
	validatorStore    selfValidatorStore
	beaconNetwork     beacon.BeaconNetwork
	beaconNode        beacon.BeaconNode
	fixedSubnets      records.Subnets
	syncInterval      time.Duration
	streamInterval    time.Duration
	updateSendTimeout time.Duration
}

type shareStorage interface {
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	Range(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool)
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata) ([]*ssvtypes.SSVShare, error)
}

type selfValidatorStore interface {
	SelfValidators() []*ssvtypes.SSVShare
}

func NewSyncer(
	logger *zap.Logger,
	shareStorage shareStorage,
	validatorStore selfValidatorStore,
	beaconNetwork beacon.BeaconNetwork,
	beaconNode beacon.BeaconNode,
	fixedSubnets records.Subnets,
	opts ...Option,
) *Syncer {
	u := &Syncer{
		logger:            logger,
		shareStorage:      shareStorage,
		validatorStore:    validatorStore,
		beaconNetwork:     beaconNetwork,
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

func (s *Syncer) SyncOnStartup(ctx context.Context) ([]*ssvtypes.SSVShare, error) {
	subnetsBuf := new(big.Int)
	ownSubnets := s.selfSubnets(subnetsBuf)

	// Load non-liquidated shares.
	shares := s.shareStorage.List(nil, registrystorage.ByNotLiquidated(), func(share *ssvtypes.SSVShare) bool {
		networkcommons.SetCommitteeSubnet(subnetsBuf, share.CommitteeID())
		subnet := subnetsBuf.Uint64()
		return ownSubnets[subnet] != 0
	})
	if len(shares) == 0 {
		s.logger.Info("could not find non-liquidated own subnets validator shares on initial metadata retrieval")
		return nil, nil
	}

	// Skip syncing if metadata was already fetched before
	// to prevent blocking startup after first sync.
	needToSync := false
	pubKeysToFetch := make([]spectypes.ValidatorPK, 0, len(shares))
	for _, share := range shares {
		pubKeysToFetch = append(pubKeysToFetch, share.ValidatorPubKey)
		if !share.HasBeaconMetadata() {
			needToSync = true
		}
	}
	if !needToSync {
		// Stream should take it over from here.
		return nil, nil
	}

	// Sync all pubkeys that belong to own subnets. We don't need to batch them because we need to wait here until all of them are synced.
	return s.Sync(ctx, pubKeysToFetch)
}

func (s *Syncer) Sync(ctx context.Context, pubKeys []spectypes.ValidatorPK) ([]*ssvtypes.SSVShare, error) {
	fetchStart := time.Now()
	metadata, err := s.Fetch(ctx, pubKeys)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}

	s.logger.Debug("🆕 fetched validators metadata",
		fields.Took(time.Since(fetchStart)),
		zap.Int("metadatas", len(metadata)),
		zap.Int("validators", len(pubKeys)),
	)

	updateStart := time.Now()
	// TODO: Refactor share storage to support passing context.
	updatedValidators, err := s.shareStorage.UpdateValidatorsMetadata(metadata)
	if err != nil {
		return nil, fmt.Errorf("update metadata: %w", err)
	}

	s.logger.Debug("🆕 saved validators metadata",
		fields.Took(time.Since(updateStart)),
		zap.Int("metadatas", len(metadata)),
		zap.Int("validators", len(updatedValidators)),
	)

	return updatedValidators, nil
}

func (s *Syncer) Fetch(_ context.Context, pubKeys []spectypes.ValidatorPK) (map[spectypes.ValidatorPK]*beacon.ValidatorMetadata, error) {
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

			if len(batch.SharesAfter) == 0 {
				if slept := s.sleep(ctx, s.streamInterval); !slept {
					return
				}
				continue
			}

			// TODO: use time.After when Go is updated to 1.23
			timer := time.NewTimer(s.updateSendTimeout)
			select {
			case metadataUpdates <- batch:
				// Only sleep if there aren't more validators to fetch metadata for.
				// It's done to wait for some data to appear. Without sleep, the next batch would likely be empty.
				if done {
					if slept := s.sleep(ctx, s.streamInterval); !slept {
						// canceled context
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
func (s *Syncer) syncNextBatch(ctx context.Context, subnetsBuf *big.Int) (SyncBatch, bool, error) {
	// TODO: Methods called here don't handle context, so this is a workaround to handle done context. It should be removed once ctx is handled gracefully.
	select {
	case <-ctx.Done():
		return SyncBatch{}, false, ctx.Err()
	default:
	}

	sharesBefore := s.nextBatchFromDB(ctx, subnetsBuf)
	if len(sharesBefore) == 0 {
		return SyncBatch{}, false, nil
	}

	pubKeys := make([]spectypes.ValidatorPK, len(sharesBefore))
	for i, share := range sharesBefore {
		pubKeys[i] = share.ValidatorPubKey
	}

	syncEpoch := s.beaconNetwork.EstimatedCurrentEpoch()
	sharesAfter, err := s.Sync(ctx, pubKeys)
	if err != nil {
		return SyncBatch{}, false, fmt.Errorf("sync: %w", err)
	}

	syncBatch := SyncBatch{
		SharesBefore: sharesBefore,
		SharesAfter:  sharesAfter,
		Epoch:        syncEpoch,
	}

	return syncBatch, len(sharesBefore) < batchSize, nil
}

// nextBatchFromDB returns non-liquidated shares from DB that are most deserving of an update, it relies on share.Metadata.lastUpdated to be updated in order to keep iterating forward.
func (s *Syncer) nextBatchFromDB(_ context.Context, subnetsBuf *big.Int) []*ssvtypes.SSVShare {
	// TODO: use context, return if it's done
	ownSubnets := s.selfSubnets(subnetsBuf)

	var staleShares, newShares []*ssvtypes.SSVShare
	s.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.Liquidated {
			return true
		}

		networkcommons.SetCommitteeSubnet(subnetsBuf, share.CommitteeID())
		subnet := subnetsBuf.Uint64()
		if ownSubnets[subnet] == 0 {
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

		// Stop iteration immediately when batchSize is reached
		return len(newShares)+len(staleShares) < batchSize
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

	// Record update time for selected shares.
	for _, share := range shares {
		share.BeaconMetadataLastUpdated = time.Now()
	}

	return shares
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

// selfSubnets calculates the operator's subnets by adding up the fixed subnets and the active committees
// it recvs big int buffer for memory reusing, if is nil it will allocate new
func (s *Syncer) selfSubnets(buf *big.Int) records.Subnets {
	// Start off with a copy of the fixed subnets (e.g., exporter subscribed to all subnets).
	localBuf := buf
	if localBuf == nil {
		localBuf = new(big.Int)
	}

	mySubnets := make(records.Subnets, networkcommons.SubnetsCount)
	copy(mySubnets, s.fixedSubnets)

	// Compute the new subnets according to the active committees/validators.
	myValidators := s.validatorStore.SelfValidators()
	for _, v := range myValidators {
		networkcommons.SetCommitteeSubnet(localBuf, v.CommitteeID())
		mySubnets[localBuf.Uint64()] = 1
	}

	return mySubnets
}
