package metadata

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	networkcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
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
	validatorStore    registrystorage.ValidatorStore
	beaconNode        beacon.BeaconNode
	fixedSubnets      networkcommons.Subnets
	syncInterval      time.Duration
	streamInterval    time.Duration
	updateSendTimeout time.Duration
}

func NewSyncer(
	logger *zap.Logger,
	validatorStore registrystorage.ValidatorStore,
	beaconNode beacon.BeaconNode,
	fixedSubnets networkcommons.Subnets,
	opts ...Option,
) *Syncer {
	u := &Syncer{
		logger:            logger,
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

func (s *Syncer) SyncOnStartup(ctx context.Context) (beacon.ValidatorMetadataMap, error) {
	subnetsBuf := new(big.Int)
	ownSubnets := s.selfSubnets(subnetsBuf)

	// Get all validators from the store
	allValidators := s.validatorStore.GetAllValidators()

	// Filter for non-liquidated validators in own subnets
	var validatorsToSync []*registrystorage.ValidatorSnapshot
	needToSync := false

	for _, snapshot := range allValidators {
		// Skip liquidated validators
		if snapshot.Share.Liquidated {
			continue
		}

		// Check if validator belongs to our subnets
		networkcommons.SetCommitteeSubnet(subnetsBuf, snapshot.Share.CommitteeID())
		subnet := subnetsBuf.Uint64()
		if !ownSubnets.IsSet(subnet) {
			continue
		}

		validatorsToSync = append(validatorsToSync, snapshot)

		// Check if any validator needs metadata sync
		if !snapshot.Share.HasBeaconMetadata() {
			needToSync = true
		}
	}

	if len(validatorsToSync) == 0 {
		s.logger.Info("could not find non-liquidated own subnets validator shares on initial metadata retrieval")
		return nil, nil
	}

	// Skip syncing if metadata was already fetched before
	// to prevent blocking startup after first sync.
	if !needToSync {
		// Stream should take it over from here.
		return nil, nil
	}

	// Extract public keys for sync
	pubKeysToFetch := make([]spectypes.ValidatorPK, 0, len(validatorsToSync))
	for _, snapshot := range validatorsToSync {
		pubKeysToFetch = append(pubKeysToFetch, snapshot.Share.ValidatorPubKey)
	}

	// Sync all pubkeys that belong to own subnets. We don't need to batch them because we need to wait here until all of them are synced.
	return s.Sync(ctx, pubKeysToFetch)
}

// Sync retrieves metadata for the provided public keys and updates storage accordingly.
// Returns updated metadata for keys that had changes. Returns nil if no keys were provided or no updates occurred.
func (s *Syncer) Sync(ctx context.Context, pubKeys []spectypes.ValidatorPK) (beacon.ValidatorMetadataMap, error) {
	fetchStart := time.Now()
	metadata, err := s.Fetch(ctx, pubKeys)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}

	s.logger.Debug("🆕 fetched validators metadata",
		fields.Took(time.Since(fetchStart)),
		zap.Int("requested_count", len(pubKeys)),
		zap.Int("received_count", len(metadata)),
	)

	updateStart := time.Now()
	updatedValidators, err := s.validatorStore.UpdateValidatorsMetadata(ctx, metadata)
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

func (s *Syncer) Fetch(ctx context.Context, pubKeys []spectypes.ValidatorPK) (beacon.ValidatorMetadataMap, error) {
	if len(pubKeys) == 0 {
		return nil, nil
	}

	blsPubKeys := make([]phase0.BLSPubKey, len(pubKeys))
	for i, pk := range pubKeys {
		blsPubKeys[i] = phase0.BLSPubKey(pk)
	}

	validatorsIndexMap, err := s.beaconNode.GetValidatorData(ctx, blsPubKeys)
	if err != nil {
		return nil, fmt.Errorf("get validator data from beacon node: %w", err)
	}

	results := make(beacon.ValidatorMetadataMap, len(validatorsIndexMap))
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

			if len(batch.After) == 0 {
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

	// Get all validators
	allValidators := s.validatorStore.GetAllValidators()

	var staleSnapshots, newSnapshots []*registrystorage.ValidatorSnapshot

	// Iterate through all validators to find those needing updates
	for _, snapshot := range allValidators {
		// Skip liquidated validators
		if snapshot.Share.Liquidated {
			continue
		}

		// Check if validator belongs to our subnets
		networkcommons.SetCommitteeSubnet(subnetsBuf, snapshot.Share.CommitteeID())
		subnet := subnetsBuf.Uint64()
		if !ownSubnets.IsSet(subnet) {
			continue
		}

		// Fetch new and stale shares only.
		if !snapshot.Share.HasBeaconMetadata() && snapshot.Share.BeaconMetadataLastUpdated.IsZero() {
			// Metadata was never fetched for this share, so it's considered new.
			newSnapshots = append(newSnapshots, snapshot)

			// Early exit if we have enough new shares for a batch
			if len(newSnapshots) >= batchSize {
				break
			}
		} else if time.Since(snapshot.Share.BeaconMetadataLastUpdated) > s.syncInterval {
			// Metadata hasn't been fetched for a while, so it's considered stale.
			staleSnapshots = append(staleSnapshots, snapshot)
		}
	}

	// Combine validators up to batchSize, prioritizing the new ones.
	snapshots := append(newSnapshots, staleSnapshots...)
	if len(snapshots) > batchSize {
		snapshots = snapshots[:batchSize]
	}

	// Build metadata map from selected snapshots
	metadataMap := make(beacon.ValidatorMetadataMap, len(snapshots))
	for _, snapshot := range snapshots {
		metadataMap[snapshot.Share.ValidatorPubKey] = snapshot.Share.BeaconMetadata()
	}

	return metadataMap
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

	mySubnets := s.fixedSubnets

	// Compute the new subnets according to the active committees/validators.
	selfValidators := s.validatorStore.GetSelfValidators()
	for _, snapshot := range selfValidators {
		networkcommons.SetCommitteeSubnet(localBuf, snapshot.Share.CommitteeID())
		mySubnets.Set(localBuf.Uint64())
	}

	return mySubnets
}
