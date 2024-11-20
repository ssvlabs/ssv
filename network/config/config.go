package networkconfig

import (
	"fmt"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate mockgen -package=networkconfig -destination=./mock.go -source=./config.go

// DomainTypeProvider is an interface for getting the domain type based on the current or given epoch.
type DomainTypeProvider interface {
	DomainType() spectypes.DomainType
	NextDomainType() spectypes.DomainType
	DomainTypeAtEpoch(epoch phase0.Epoch) spectypes.DomainType
}

type Interface interface { // TODO: rename?
	DomainTypeProvider
	PastAlanFork() bool
	PastAlanForkAtEpoch(epoch phase0.Epoch) bool

	BeaconNetwork() string
	GenesisForkVersion() phase0.Version
	GenesisTime() time.Time
	SlotDuration() time.Duration
	SlotsPerEpoch() phase0.Slot
	EpochsPerSyncCommitteePeriod() phase0.Epoch
	IntervalsPerSlot() uint64

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
}

var _ Interface = NetworkConfig{}

type NetworkConfig struct {
	SSV
	Beacon
}

func (n NetworkConfig) String() string {
	return fmt.Sprintf("%#v", n)
}

func (n NetworkConfig) PastAlanFork() bool {
	return n.EstimatedCurrentEpoch() >= n.AlanForkEpoch
}

func (n NetworkConfig) PastAlanForkAtEpoch(epoch phase0.Epoch) bool {
	return epoch >= n.AlanForkEpoch
}

// SlotDuration returns slot duration
func (n NetworkConfig) SlotDuration() time.Duration {
	return n.Beacon.SlotDuration
}

// EpochDuration returns slot duration
func (n NetworkConfig) EpochDuration() time.Duration {
	slotsPerEpoch := n.Beacon.SlotsPerEpoch
	if slotsPerEpoch > math.MaxInt64 {
		panic("slot out of range")
	}
	return n.Beacon.SlotDuration * time.Duration(slotsPerEpoch) // #nosec G115: slot cannot exceed math.MaxInt64
}

// SlotsPerEpoch returns number of slots per one epoch
func (n NetworkConfig) SlotsPerEpoch() phase0.Slot {
	return n.Beacon.SlotsPerEpoch
}

// IntervalsPerSlot returns number of intervals per slot
func (n NetworkConfig) IntervalsPerSlot() uint64 {
	return n.Beacon.IntervalsPerSlot
}

func (n NetworkConfig) EpochsPerSyncCommitteePeriod() phase0.Epoch {
	return n.Beacon.EpochsPerSyncCommitteePeriod
}

// DomainType returns current domain type based on the current fork.
func (n NetworkConfig) DomainType() spectypes.DomainType {
	return n.DomainTypeAtEpoch(n.EstimatedCurrentEpoch())
}

func (n NetworkConfig) BeaconNetwork() string {
	return n.Beacon.ConfigName
}
