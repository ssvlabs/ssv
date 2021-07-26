package duties

import (
	"fmt"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

type dutyCacheEntry struct {
	Duties []beacon.Duty
}

// validatorsIndicesFetcher represents the interface for retrieving indices
type validatorsIndicesFetcher interface {
	GetValidatorsIndices() []spec.ValidatorIndex
}

// beaconDutiesClient interface of the needed client for managing duties
type beaconDutiesClient interface {
	// GetDuties returns duties for the passed validators indices
	GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	// SubscribeToCommitteeSubnet subscribe committee to subnet (p2p topic)
	SubscribeToCommitteeSubnet(subscription []*eth2apiv1.BeaconCommitteeSubscription) error
}

// DutyFetcher represents the component that manages duties
type DutyFetcher interface {
	GetDuties(slot uint64) ([]beacon.Duty, error)
}

// newDutyFetcher creates a new instance
func newDutyFetcher(logger *zap.Logger, beaconClient beaconDutiesClient, indicesFetcher validatorsIndicesFetcher, network core.Network) DutyFetcher {
	df := dutyFetcher{
		logger:         logger.With(zap.String("component", "operator/dutyFetcher")),
		ethNetwork:     network,
		beaconClient:   beaconClient,
		indicesFetcher: indicesFetcher,
		cache:          cache.New(time.Minute*30, time.Minute*31),
	}
	return &df
}

// dutyFetcher is internal implementation of DutyFetcher
type dutyFetcher struct {
	logger         *zap.Logger
	ethNetwork     core.Network
	beaconClient   beaconDutiesClient
	indicesFetcher validatorsIndicesFetcher

	cache *cache.Cache
}

// GetDuties tries to get slot's duties from cache, if not available in cache it fetches them from beacon
// the relevant subnets will be subscribed once duties are fetched
func (df *dutyFetcher) GetDuties(slot uint64) ([]beacon.Duty, error) {
	var duties []beacon.Duty

	logger := df.logger.With(zap.Uint64("slot", slot))

	cacheKey := getDutyCacheKey(slot)
	if raw, exist := df.cache.Get(cacheKey); exist {
		logger.Debug("found duties in cache")
		duties = raw.(dutyCacheEntry).Duties
	} else { // does not exist in cache -> fetch
		logger.Debug("no entry in cache, fetching duties from beacon node")
		if err := df.updateDutiesFromBeacon(slot); err != nil {
			logger.Error("failed to get duties", zap.Error(err))
			return nil, err
		}
		if raw, exist := df.cache.Get(cacheKey); exist {
			duties = raw.(dutyCacheEntry).Duties
		}
	}

	logger.Debug("found duties for slot",
		zap.Int("count", len(duties)), zap.Any("duties", duties))

	return duties, nil
}

// updateDutiesFromBeacon will be called once in an epoch to update the cache with all the epoch's slots
func (df *dutyFetcher) updateDutiesFromBeacon(slot uint64) error {
	attesterDuties, err := df.fetchAttesterDuties(slot)
	if err != nil {
		return errors.Wrap(err, "failed to get attest duties")
	}
	df.logger.Debug("got duties", zap.Int("count", len(attesterDuties)), zap.Any("attesterDuties", attesterDuties))
	if err := df.processFetchedDuties(attesterDuties); err != nil {
		return errors.Wrap(err, "failed to process fetched duties")
	}
	return nil
}

// fetchAttesterDuties fetches duties for the epoch of the given slot
func (df *dutyFetcher) fetchAttesterDuties(slot uint64) ([]*eth2apiv1.AttesterDuty, error) {
	if indices := df.indicesFetcher.GetValidatorsIndices(); len(indices) > 0 {
		df.logger.Debug("got indices for existing validators",
			zap.Int("count", len(indices)), zap.Any("indices", indices))
		esEpoch := df.ethNetwork.EstimatedEpochAtSlot(slot)
		epoch := spec.Epoch(esEpoch)
		return df.beaconClient.GetDuties(epoch, indices)
	}
	df.logger.Debug("got no indices, duties won't be fetched")
	return []*eth2apiv1.AttesterDuty{}, nil
}

// processFetchedDuties loop over fetched duties and process them
func (df *dutyFetcher) processFetchedDuties(attesterDuties []*eth2apiv1.AttesterDuty) error {
	var subscriptions []*eth2apiv1.BeaconCommitteeSubscription
	entries := map[spec.Slot]dutyCacheEntry{}
	for _, ad := range attesterDuties {
		duty := convertAttesterDuty(ad)
		df.createEntry(entries, duty)
		subscriptions = append(subscriptions, toSubscription(duty))
	}
	df.populateCache(entries)
	if err := df.beaconClient.SubscribeToCommitteeSubnet(subscriptions); err != nil {
		df.logger.Warn("failed to subscribe committee to subnet", zap.Error(err))
		//	 TODO should add return? if so could end up inserting redundant duties
	}
	return nil
}

func (df *dutyFetcher) createEntry(entries map[spec.Slot]dutyCacheEntry, duty *beacon.Duty) {
	entry, slotExist := entries[duty.Slot]
	if !slotExist {
		entry = dutyCacheEntry{[]beacon.Duty{}}
	}
	entry.Duties = append(entry.Duties, *duty)
	entries[duty.Slot] = entry
}

// populateCache takes a map of entries and updates the cache
func (df *dutyFetcher) populateCache(entriesToAdd map[spec.Slot]dutyCacheEntry) {
	for s, e := range entriesToAdd {
		slot := uint64(s)
		if raw, exist := df.cache.Get(getDutyCacheKey(slot)); exist {
			existingEntry := raw.(dutyCacheEntry)
			e.Duties = append(existingEntry.Duties, e.Duties...)
		}
		df.cache.SetDefault(getDutyCacheKey(slot), e)
	}
}

func getDutyCacheKey(slot uint64) string {
	return fmt.Sprintf("%d", slot)
}

func convertAttesterDuty(attesterDuty *eth2apiv1.AttesterDuty) *beacon.Duty {
	duty := beacon.Duty{
		Type:                    beacon.RoleTypeAttester,
		PubKey:                  attesterDuty.PubKey,
		Slot:                    attesterDuty.Slot,
		ValidatorIndex:          attesterDuty.ValidatorIndex,
		CommitteeIndex:          attesterDuty.CommitteeIndex,
		CommitteeLength:         attesterDuty.CommitteeLength,
		CommitteesAtSlot:        attesterDuty.CommitteesAtSlot,
		ValidatorCommitteeIndex: attesterDuty.ValidatorCommitteeIndex,
	}
	return &duty
}

func toSubscription(duty *beacon.Duty) *eth2apiv1.BeaconCommitteeSubscription {
	return &eth2apiv1.BeaconCommitteeSubscription{
		ValidatorIndex:   duty.ValidatorIndex,
		Slot:             duty.Slot,
		CommitteeIndex:   duty.CommitteeIndex,
		CommitteesAtSlot: duty.CommitteesAtSlot,
		IsAggregator:     false, // TODO need to handle agg case
	}
}
