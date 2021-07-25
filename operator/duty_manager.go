package operator

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

// GetValidatorsIndices is a function interface for retrieving indices
type GetValidatorsIndices func() []spec.ValidatorIndex

// BeaconDutiesClient interface of the needed client for managing duties
type BeaconDutiesClient interface {
	// GetDuties returns duties for the passed validators indices
	GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	// SubscribeToCommitteeSubnet subscribe committee to subnet (p2p topic)
	SubscribeToCommitteeSubnet(subscription []*eth2apiv1.BeaconCommitteeSubscription) error
}

// DutyManager represents the component that manages duties
type DutyManager interface {
	GetDuties(slot uint64) ([]beacon.Duty, error)
}

// NewDutyManager creates a new instance
func NewDutyManager(logger *zap.Logger, beaconClient BeaconDutiesClient, getValidatorsIndices GetValidatorsIndices, network core.Network) DutyManager {
	dm := dutyManager{
		logger:               logger.With(zap.String("component", "operator/dutyManager")),
		ethNetwork:           network,
		beaconClient:         beaconClient,
		getValidatorsIndices: getValidatorsIndices,
		cache:                cache.New(time.Minute*30, time.Minute*31),
	}
	return &dm
}

type dutyManager struct {
	logger               *zap.Logger
	ethNetwork           core.Network
	beaconClient         BeaconDutiesClient
	getValidatorsIndices GetValidatorsIndices

	cache *cache.Cache
}

// GetDuties tries to get slot's duties from cache, if not available in cache it fetches them from beacon
// the relevant subnets will be subscribed once duties are fetched
func (dm *dutyManager) GetDuties(slot uint64) ([]beacon.Duty, error) {
	var duties []beacon.Duty

	logger := dm.logger.With(zap.Uint64("slot", slot))

	cacheKey := getDutyCacheKey(slot)
	if raw, exist := dm.cache.Get(cacheKey); exist {
		logger.Debug("found duties in cache")
		duties = raw.(dutyCacheEntry).Duties
	} else { // does not exist in cache -> fetch
		logger.Debug("no entry in cache, fetching duties from beacon node")
		if err := dm.updateDutiesFromBeacon(slot); err != nil {
			logger.Error("failed to get duties", zap.Error(err))
			return nil, err
		}
		if raw, exist := dm.cache.Get(cacheKey); exist {
			duties = raw.(dutyCacheEntry).Duties
		}
	}

	logger.Debug("found duties for slot",
		zap.Int("count", len(duties)), zap.Any("duties", duties))

	return duties, nil
}

func (dm *dutyManager) updateDutiesFromBeacon(slot uint64) error {
	attesterDuties, err := dm.fetchAttesterDuties(slot)
	if err != nil {
		return errors.Wrap(err, "failed to get attest duties")
	}
	dm.logger.Debug("got duties", zap.Int("count", len(attesterDuties)), zap.Any("attesterDuties", attesterDuties))
	if err := dm.processFetchedDuties(attesterDuties); err != nil {
		return errors.Wrap(err, "failed to process fetched duties")
	}
	return nil
}

func (dm *dutyManager) fetchAttesterDuties(slot uint64) ([]*eth2apiv1.AttesterDuty, error) {
	if indices := dm.getValidatorsIndices(); len(indices) > 0 {
		dm.logger.Debug("got indices for existing validators",
			zap.Int("count", len(indices)), zap.Any("indices", indices))
		esEpoch := dm.ethNetwork.EstimatedEpochAtSlot(slot)
		epoch := spec.Epoch(esEpoch)
		return dm.beaconClient.GetDuties(epoch, indices)
	}
	dm.logger.Debug("got no indices, duties won't be fetched")
	return []*eth2apiv1.AttesterDuty{}, nil
}

func (dm *dutyManager) processFetchedDuties(attesterDuties []*eth2apiv1.AttesterDuty) error {
	var subscriptions []*eth2apiv1.BeaconCommitteeSubscription
	entries := map[spec.Slot]dutyCacheEntry{}
	for _, ad := range attesterDuties {
		duty := convertAttesterDuty(ad)
		dm.createEntry(entries, duty)
		subscriptions = append(subscriptions, toSubscription(duty))
	}
	dm.populateCache(entries)
	if err := dm.beaconClient.SubscribeToCommitteeSubnet(subscriptions); err != nil {
		dm.logger.Warn("failed to subscribe committee to subnet", zap.Error(err))
		//	 TODO should add return? if so could end up inserting redundant duties
	}
	return nil
}

func (dm *dutyManager) createEntry(entries map[spec.Slot]dutyCacheEntry, duty *beacon.Duty) {
	entry, slotExist := entries[duty.Slot]
	if !slotExist {
		entry = dutyCacheEntry{[]beacon.Duty{}}
	}
	entry.Duties = append(entry.Duties, *duty)
	entries[duty.Slot] = entry
}

func (dm *dutyManager) populateCache(entriesToAdd map[spec.Slot]dutyCacheEntry) {
	for s, e := range entriesToAdd {
		slot := uint64(s)
		if raw, exist := dm.cache.Get(getDutyCacheKey(slot)); exist {
			existingEntry := raw.(dutyCacheEntry)
			e.Duties = append(existingEntry.Duties, e.Duties...)
		}
		dm.cache.SetDefault(getDutyCacheKey(slot), e)
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
