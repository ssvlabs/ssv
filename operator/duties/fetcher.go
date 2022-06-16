package duties

import (
	"encoding/hex"
	"fmt"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	types "github.com/prysmaticlabs/eth2-types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

//go:generate mockgen -package=mocks -destination=./mocks/fetcher.go -source=./fetcher.go

// cacheEntry
type cacheEntry struct {
	Duties []beacon.Duty
}

// validatorsIndicesFetcher represents the interface for retrieving indices.
// It have a minimal interface instead of working with the complete validator.IController interface
type validatorsIndicesFetcher interface {
	GetValidatorsIndices() []spec.ValidatorIndex
}

// beaconDutiesClient interface of the needed client for managing duties
type beaconDutiesClient interface {
	// GetDuties returns duties for the passed validators indices
	GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*beacon.Duty, error)
	// SubscribeToCommitteeSubnet subscribe committee to subnet (p2p topic)
	SubscribeToCommitteeSubnet(subscription []*eth2apiv1.BeaconCommitteeSubscription) error
}

// DutyFetcher represents the component that manages duties
type DutyFetcher interface {
	GetDuties(slot uint64) ([]beacon.Duty, error)
}

// newDutyFetcher creates a new instance
func newDutyFetcher(logger *zap.Logger, beaconClient beaconDutiesClient, indicesFetcher validatorsIndicesFetcher, network beacon.Network) DutyFetcher {
	df := dutyFetcher{
		logger:         logger.With(zap.String("component", "operator/dutyFetcher")),
		ethNetwork:     network,
		beaconClient:   beaconClient,
		indicesFetcher: indicesFetcher,
		cache:          cache.New(time.Minute*12, time.Minute*13),
	}
	return &df
}

// dutyFetcher is internal implementation of DutyFetcher
type dutyFetcher struct {
	logger         *zap.Logger
	ethNetwork     beacon.Network
	beaconClient   beaconDutiesClient
	indicesFetcher validatorsIndicesFetcher

	cache *cache.Cache
}

// GetDuties tries to get slot's duties from cache, if not available in cache it fetches them from beacon
// the relevant subnets will be subscribed once duties are fetched
func (df *dutyFetcher) GetDuties(slot uint64) ([]beacon.Duty, error) {
	var duties []beacon.Duty

	esEpoch := df.ethNetwork.EstimatedEpochAtSlot(types.Slot(slot))
	epoch := spec.Epoch(esEpoch)
	logger := df.logger.With(zap.Uint64("slot", slot), zap.Uint64("epoch", uint64(epoch)))
	start := time.Now()
	cacheKey := getDutyCacheKey(slot)
	if raw, exist := df.cache.Get(cacheKey); exist {
		duties = raw.(cacheEntry).Duties
	} else {
		// epoch's duties does not exist in cache -> fetch
		if err := df.updateDutiesFromBeacon(slot); err != nil {
			logger.Warn("failed to get duties", zap.Error(err))
			return nil, err
		}
		if raw, exist := df.cache.Get(cacheKey); exist {
			duties = raw.(cacheEntry).Duties
		}
	}
	if len(duties) > 0 {
		logger.Debug("found duties for slot",
			zap.Int("count", len(duties)), zap.Any("duties", duties),
			zap.Duration("duration", time.Since(start)))
	}

	return duties, nil
}

// updateDutiesFromBeacon will be called once in an epoch to update the cache with all the epoch's slots
func (df *dutyFetcher) updateDutiesFromBeacon(slot uint64) error {
	duties, err := df.fetchDuties(slot)
	if err != nil {
		return errors.Wrap(err, "failed to get duties from beacon")
	}
	if len(duties) == 0 {
		return nil
	}
	// print the newly fetched duties
	var toPrint []serializedDuty
	for _, d := range duties {
		toPrint = append(toPrint, toSerialized(d))
	}
	df.logger.Debug("got duties", zap.Int("count", len(duties)), zap.Any("duties", toPrint))

	if err := df.processFetchedDuties(duties); err != nil {
		return errors.Wrap(err, "failed to process fetched duties")
	}

	return nil
}

// fetchDuties fetches duties for the epoch of the given slot
func (df *dutyFetcher) fetchDuties(slot uint64) ([]*beacon.Duty, error) {
	if indices := df.indicesFetcher.GetValidatorsIndices(); len(indices) > 0 {
		df.logger.Debug("got indices for existing validators",
			zap.Int("count", len(indices)), zap.Any("indices", indices))
		esEpoch := df.ethNetwork.EstimatedEpochAtSlot(types.Slot(slot))
		epoch := spec.Epoch(esEpoch)
		results, err := df.beaconClient.GetDuties(epoch, indices)
		return results, err
	}
	df.logger.Debug("no indices, duties won't be fetched")
	return []*beacon.Duty{}, nil
}

// processFetchedDuties loop over fetched duties and process them
func (df *dutyFetcher) processFetchedDuties(fetchedDuties []*beacon.Duty) error {
	if len(fetchedDuties) > 0 {
		var subscriptions []*eth2apiv1.BeaconCommitteeSubscription
		// entries holds all the new duties to add
		entries := map[spec.Slot]cacheEntry{}
		for _, duty := range fetchedDuties {
			df.fillEntry(entries, duty)
			subscriptions = append(subscriptions, toSubscription(duty))
		}
		df.populateCache(entries)
		if err := df.beaconClient.SubscribeToCommitteeSubnet(subscriptions); err != nil {
			df.logger.Warn("failed to subscribe committee to subnet", zap.Error(err))
		}
	}
	return nil
}

// fillEntry adds the given duty on the relevant slot
func (df *dutyFetcher) fillEntry(entries map[spec.Slot]cacheEntry, duty *beacon.Duty) {
	entry, slotExist := entries[duty.Slot]
	if !slotExist {
		entry = cacheEntry{[]beacon.Duty{}}
	}
	entry.Duties = append(entry.Duties, *duty)
	entries[duty.Slot] = entry
}

// populateCache takes a map of entries and updates the cache
func (df *dutyFetcher) populateCache(entriesToAdd map[spec.Slot]cacheEntry) {
	df.addMissingSlots(entriesToAdd)
	for s, e := range entriesToAdd {
		slot := uint64(s)
		if raw, exist := df.cache.Get(getDutyCacheKey(slot)); exist {
			var dutiesToAdd []beacon.Duty
			existingEntry := raw.(cacheEntry)
			for _, newDuty := range e.Duties {
				exist := false
				for _, existDuty := range existingEntry.Duties {
					if newDuty.ValidatorIndex == existDuty.ValidatorIndex {
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

func (df *dutyFetcher) addMissingSlots(entries map[spec.Slot]cacheEntry) {
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
		s := spec.Slot(epochFirstSlot + uint64(i))
		if _, exist := entries[s]; !exist {
			entries[s] = cacheEntry{[]beacon.Duty{}}
		}
	}
}

func (df *dutyFetcher) firstSlotOfEpoch(slot uint64) uint64 {
	mod := slot % df.ethNetwork.SlotsPerEpoch()
	return slot - mod
}

// getDutyCacheKey return the cache key for a slot
func getDutyCacheKey(slot uint64) string {
	return fmt.Sprintf("d-%d", slot)
}

// toSubscription creates a subscription from the given duty
func toSubscription(duty *beacon.Duty) *eth2apiv1.BeaconCommitteeSubscription {
	return &eth2apiv1.BeaconCommitteeSubscription{
		ValidatorIndex:   duty.ValidatorIndex,
		Slot:             duty.Slot,
		CommitteeIndex:   duty.CommitteeIndex,
		CommitteesAtSlot: duty.CommitteesAtSlot,
		IsAggregator:     false, // TODO need to handle agg case
	}
}

type serializedDuty struct {
	PubKey                  string
	Type                    string
	Slot                    uint64
	ValidatorIndex          uint64
	CommitteeIndex          uint64
	CommitteeLength         uint64
	CommitteesAtSlot        uint64
	ValidatorCommitteeIndex uint64
}

func toSerialized(d *beacon.Duty) serializedDuty {
	return serializedDuty{
		PubKey:                  hex.EncodeToString(d.PubKey[:]),
		Type:                    d.Type.String(),
		Slot:                    uint64(d.Slot),
		ValidatorIndex:          uint64(d.ValidatorIndex),
		CommitteeIndex:          uint64(d.CommitteeIndex),
		CommitteeLength:         d.CommitteeLength,
		CommitteesAtSlot:        d.CommitteesAtSlot,
		ValidatorCommitteeIndex: d.ValidatorCommitteeIndex,
	}
}
