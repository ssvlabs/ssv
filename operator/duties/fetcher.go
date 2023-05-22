package duties

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bloxapp/ssv/logging/fields"

	eth2client "github.com/attestantio/go-eth2-client"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

//go:generate mockgen -package=mocks -destination=./mocks/fetcher.go -source=./fetcher.go

// cacheEntry
type cacheEntry struct {
	Duties []spectypes.Duty
}

// validatorsIndicesFetcher represents the interface for retrieving indices.
// It have a minimal interface instead of working with the complete validator.IController interface
type validatorsIndicesFetcher interface {
	GetValidatorsIndices(logger *zap.Logger) []phase0.ValidatorIndex
}

// DutyFetcher represents the component that manages duties
type DutyFetcher interface {
	GetDuties(logger *zap.Logger, slot phase0.Slot) ([]spectypes.Duty, error)
	SyncCommitteeDuties(logger *zap.Logger, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error)
	eth2client.EventsProvider
}

// newDutyFetcher creates a new instance
func newDutyFetcher(logger *zap.Logger, beaconClient beacon.Beacon, indicesFetcher validatorsIndicesFetcher, network beacon.Network) DutyFetcher {
	df := dutyFetcher{
		logger:         logger,
		ethNetwork:     network,
		beaconClient:   beaconClient,
		indicesFetcher: indicesFetcher,
		cache:          cache.New(time.Minute*12, time.Minute*13),
	}
	return &df
}

// dutyFetcher is internal implementation of DutyFetcher
type dutyFetcher struct {
	logger         *zap.Logger // struct logger to implement eth2client.EventsProvider
	ethNetwork     beacon.Network
	beaconClient   beacon.Beacon
	indicesFetcher validatorsIndicesFetcher

	cache *cache.Cache
}

func (df *dutyFetcher) Events(ctx context.Context, topics []string, handler eth2client.EventHandlerFunc) error {
	df.logger.Debug("subscribing to events", zap.Any("topics", topics))
	return df.beaconClient.Events(ctx, topics, handler)
}

// GetDuties tries to get slot's duties from cache, if not available in cache it fetches them from beacon
// the relevant subnets will be subscribed once duties are fetched
func (df *dutyFetcher) GetDuties(logger *zap.Logger, slot phase0.Slot) ([]spectypes.Duty, error) {
	var duties []spectypes.Duty

	epoch := df.ethNetwork.EstimatedEpochAtSlot(slot)
	logger = logger.With(fields.Slot(slot), zap.Uint64("epoch", uint64(epoch)))
	start := time.Now()
	cacheKey := getDutyCacheKey(slot)
	if raw, exist := df.cache.Get(cacheKey); exist {
		duties = raw.(cacheEntry).Duties
	} else {
		// epoch's duties does not exist in cache -> fetch
		if err := df.updateDutiesFromBeacon(logger, slot); err != nil {
			logger.Warn("failed to get duties", zap.Error(err))
			return nil, err
		}
		if raw, exist := df.cache.Get(cacheKey); exist {
			duties = raw.(cacheEntry).Duties
		}
	}
	if len(duties) > 0 {
		logger.Debug("found duties for slot",
			zap.Int("count", len(duties)), // zap.Any("duties", duties),
			fields.Duration(start))
	}

	return duties, nil
}

// updateDutiesFromBeacon will be called once in an epoch to update the cache with all the epoch's slots
func (df *dutyFetcher) updateDutiesFromBeacon(logger *zap.Logger, slot phase0.Slot) error {
	start := time.Now()
	duties, err := df.fetchDuties(logger, slot)
	if err != nil {
		return errors.Wrap(err, "failed to get duties from beacon")
	}
	if len(duties) == 0 {
		return nil
	}

	var b strings.Builder
	for i, duty := range duties {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fields.FormatDutyID(df.ethNetwork.EstimatedEpochAtSlot(slot), duty))
	}
	logger.Debug("got duties",
		zap.Int("count", len(duties)),
		zap.Any("duties", b.String()),
		fields.Duration(start))

	if err := df.processFetchedDuties(logger, duties); err != nil {
		return errors.Wrap(err, "failed to process fetched duties")
	}

	return nil
}

// fetchDuties fetches duties for the epoch of the given slot
func (df *dutyFetcher) fetchDuties(logger *zap.Logger, slot phase0.Slot) ([]*spectypes.Duty, error) {
	if indices := df.indicesFetcher.GetValidatorsIndices(logger); len(indices) > 0 {
		logger.Debug("got indices for existing validators",
			zap.Int("count", len(indices)), zap.Any("indices", indices))
		epoch := df.ethNetwork.EstimatedEpochAtSlot(slot)
		results, err := df.beaconClient.GetDuties(logger, epoch, indices)
		return results, err
	}
	logger.Debug("no indices, duties won't be fetched")
	return []*spectypes.Duty{}, nil
}

// processFetchedDuties loop over fetched duties and process them
func (df *dutyFetcher) processFetchedDuties(logger *zap.Logger, fetchedDuties []*spectypes.Duty) error {
	if len(fetchedDuties) > 0 {
		var subscriptions []*eth2apiv1.BeaconCommitteeSubscription
		// entries holds all the new duties to add
		entries := map[phase0.Slot]cacheEntry{}
		for _, duty := range fetchedDuties {
			df.fillEntry(entries, duty)
			subscriptions = append(subscriptions, toSubscription(duty))
		}

		df.populateCache(entries)

		if err := df.beaconClient.SubscribeToCommitteeSubnet(subscriptions); err != nil {
			logger.Warn("failed to subscribe committee to subnet", zap.Error(err))
		}
	}
	return nil
}

// fillEntry adds the given duty on the relevant slot
func (df *dutyFetcher) fillEntry(entries map[phase0.Slot]cacheEntry, duty *spectypes.Duty) {
	entry, slotExist := entries[duty.Slot]
	if !slotExist {
		entry = cacheEntry{[]spectypes.Duty{}}
	}
	entry.Duties = append(entry.Duties, *duty)
	entries[duty.Slot] = entry
}

// populateCache takes a map of entries and updates the cache
func (df *dutyFetcher) populateCache(entriesToAdd map[phase0.Slot]cacheEntry) {
	df.addMissingSlots(entriesToAdd)
	for s, e := range entriesToAdd {
		slot := s
		if raw, exist := df.cache.Get(getDutyCacheKey(slot)); exist {
			var dutiesToAdd []spectypes.Duty
			existingEntry := raw.(cacheEntry)
			for _, newDuty := range e.Duties {
				exist := false
				for _, existDuty := range existingEntry.Duties {
					if newDuty.ValidatorIndex == existDuty.ValidatorIndex && newDuty.Type == existDuty.Type {
						exist = true
						break // already exist, pass
					}
				}
				if !exist {
					dutiesToAdd = append(dutiesToAdd, newDuty)
				}
			}
			e.Duties = append(existingEntry.Duties, dutiesToAdd...)
		}
		df.cache.SetDefault(getDutyCacheKey(slot), e)
	}
}

func (df *dutyFetcher) addMissingSlots(entries map[phase0.Slot]cacheEntry) {
	if len(entries) == int(df.ethNetwork.SlotsPerEpoch()) {
		// in case all slots exist -> do nothing
		return
	}
	// takes some slot from current epoch
	var slot uint64
	for s := range entries {
		slot = uint64(s)
		break
	}
	epochFirstSlot := df.firstSlotOfEpoch(slot)
	// add all missing slots
	for i := 0; i < int(df.ethNetwork.SlotsPerEpoch()); i++ {
		s := phase0.Slot(epochFirstSlot + uint64(i))
		if _, exist := entries[s]; !exist {
			entries[s] = cacheEntry{[]spectypes.Duty{}}
		}
	}
}

func (df *dutyFetcher) firstSlotOfEpoch(slot uint64) uint64 {
	mod := slot % df.ethNetwork.SlotsPerEpoch()
	return slot - mod
}

// getDutyCacheKey return the cache key for a slot
func getDutyCacheKey(slot phase0.Slot) string {
	return fmt.Sprintf("d-%d", slot)
}

// toSubscription creates a subscription from the given duty
func toSubscription(duty *spectypes.Duty) *eth2apiv1.BeaconCommitteeSubscription {
	return &eth2apiv1.BeaconCommitteeSubscription{
		ValidatorIndex:   duty.ValidatorIndex,
		Slot:             duty.Slot,
		CommitteeIndex:   duty.CommitteeIndex,
		CommitteesAtSlot: duty.CommitteesAtSlot,
		IsAggregator:     duty.Type == spectypes.BNRoleAggregator, // TODO call subscribe after pre-consensus (aggregate & sync committee contribution)
	}
}
