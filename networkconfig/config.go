package networkconfig

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/sanity-io/litter"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate mockgen -package=networkconfig -destination=./mock.go -source=./config.go

type Interface interface { // TODO: rename?
	BeaconNetwork() string
	DomainType() spectypes.DomainType
	GenesisForkVersion() phase0.Version
	GenesisTime() time.Time
	SlotDuration() time.Duration
	SlotsPerEpoch() phase0.Slot
	SyncCommitteeSize() uint64

	EstimatedCurrentSlot() phase0.Slot
	EstimatedCurrentEpoch() phase0.Epoch
	EstimatedSlotAtTime(time time.Time) phase0.Slot
	EstimatedTimeAtSlot(slot phase0.Slot) time.Time
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot
	EpochStartTime(epoch phase0.Epoch) time.Time
	GetSlotStartTime(slot phase0.Slot) time.Time
	GetSlotEndTime(slot phase0.Slot) time.Time
	IsFirstSlotOfEpoch(slot phase0.Slot) bool
	GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot
	EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64
	FirstEpochOfSyncPeriod(period uint64) phase0.Epoch
	LastSlotOfSyncPeriod(period uint64) phase0.Slot
	IntervalDuration() time.Duration
	SlotsPerPeriod() phase0.Slot
}

var _ Interface = NetworkConfig{}

type NetworkConfig struct {
	SSV
	Beacon
}

func (n NetworkConfig) String() string {
	return litter.Sdump(n)
}

// SlotDuration returns slot duration
func (n NetworkConfig) SlotDuration() time.Duration {
	return n.Beacon.SlotDuration
}

// SlotsPerEpoch returns number of slots per one epoch
func (n NetworkConfig) SlotsPerEpoch() phase0.Slot {
	return n.Beacon.SlotsPerEpoch
}

// DomainType returns current domain type based on the current fork.
func (n NetworkConfig) DomainType() spectypes.DomainType {
	return n.SSV.DomainType
}

func (n NetworkConfig) BeaconNetwork() string {
	return n.Beacon.NetworkName
}

func (n NetworkConfig) SyncCommitteeSize() uint64 {
	return n.Beacon.SyncCommitteeSize
}
