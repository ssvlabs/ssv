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
	"github.com/ssvlabs/ssv/networkconfig"
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
	batchSize                = 512
)

type Syncer struct {
	logger            *zap.Logger
	shareStorage      shareStorage
	validatorStore    selfValidatorStore
	networkConfig     networkconfig.NetworkConfig
	beaconNode        beacon.BeaconNode
	fixedSubnets      networkcommons.Subnets
	syncInterval      time.Duration
	streamInterval    time.Duration
	updateSendTimeout time.Duration
}

type shareStorage interface {
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	Range(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool)
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata) error
}

type selfValidatorStore interface {
	SelfValidators() []*ssvtypes.SSVShare
}

func NewSyncer(
	logger *zap.Logger,
	shareStorage shareStorage,
	validatorStore selfValidatorStore,
	networkConfig networkconfig.NetworkConfig,
	beaconNode beacon.BeaconNode,
	fixedSubnets networkcommons.Subnets,
	opts ...Option,
) *Syncer {
	u := &Syncer{
		logger:            logger,
		shareStorage:      shareStorage,
		validatorStore:    validatorStore,
		networkConfig:     networkConfig,
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

func (s *Syncer) SyncOnStartup(ctx context.Context) (map[spectypes.ValidatorPK]*beacon.ValidatorMetadata, error) {
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

	logger := s.logger.With(zap.Int("metadatas", len(metadata)), zap.Int("validators", len(pubKeys)))
	logger.Debug("🆕 fetched validators metadata", fields.Took(time.Since(fetchStart)))

	updateStart := time.Now()
	// TODO: Refactor share storage to support passing context.
	if err := s.shareStorage.UpdateValidatorsMetadata(metadata); err != nil {
		return metadata, fmt.Errorf("update metadata: %w", err)
	}

	logger.Debug("🆕 saved validators metadata", fields.Took(time.Since(updateStart)))

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
			Status:          v.Status,
			Index:           v.Index,
			ActivationEpoch: v.Validator.ActivationEpoch,
			ExitEpoch:       v.Validator.ExitEpoch,
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

	shares := s.nextBatch(ctx, subnetsBuf)
	if len(shares) == 0 {
		return SyncBatch{}, false, nil
	}

	pubKeys := make([]spectypes.ValidatorPK, len(shares))
	for i, share := range shares {
		pubKeys[i] = share.ValidatorPubKey
	}

	indicesBefore := s.allActiveIndices(ctx, s.networkConfig.Beacon.GetBeaconNetwork().EstimatedCurrentEpoch())

	validators, err := s.Sync(ctx, pubKeys)
	if err != nil {
		return SyncBatch{}, false, fmt.Errorf("sync: %w", err)
	}

	indicesAfter := s.allActiveIndices(ctx, s.networkConfig.Beacon.GetBeaconNetwork().EstimatedCurrentEpoch())

	update := SyncBatch{
		IndicesBefore: indicesBefore,
		IndicesAfter:  indicesAfter,
		Validators:    validators,
	}

	return update, len(shares) < batchSize, nil
}

// nextBatch returns non-liquidated shares from DB that are most deserving of an update, it relies on share.Metadata.lastUpdated to be updated in order to keep iterating forward.
func (s *Syncer) nextBatch(_ context.Context, subnetsBuf *big.Int) []*ssvtypes.SSVShare {
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

	// Record update time for selected shares.
	for _, share := range shares {
		share.BeaconMetadataLastUpdated = time.Now()
	}

	return shares
}

// TODO: Create a wrapper for share storage that contains all common methods like AllActiveIndices and use the wrapper.
func (s *Syncer) allActiveIndices(_ context.Context, epoch phase0.Epoch) []phase0.ValidatorIndex {
	var indices []phase0.ValidatorIndex

	// TODO: use context, return if it's done
	s.shareStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.IsParticipating(s.networkConfig, epoch) {
			indices = append(indices, share.ValidatorIndex)
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

// selfSubnets calculates the operator's subnets by adding up the fixed subnets and the active committees
// it recvs big int buffer for memory reusing, if is nil it will allocate new
func (s *Syncer) selfSubnets(buf *big.Int) networkcommons.Subnets {
	// Start off with a copy of the fixed subnets (e.g., exporter subscribed to all subnets).
	localBuf := buf
	if localBuf == nil {
		localBuf = new(big.Int)
	}

	mySubnets := make(networkcommons.Subnets, networkcommons.SubnetsCount)
	copy(mySubnets, s.fixedSubnets)

	// Compute the new subnets according to the active committees/validators.
	myValidators := s.validatorStore.SelfValidators()
	for _, v := range myValidators {
		networkcommons.SetCommitteeSubnet(localBuf, v.CommitteeID())
		mySubnets[localBuf.Uint64()] = 1
	}

	return mySubnets
}
