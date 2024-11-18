package networkconfig

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/config.go -source=./config.go

var SupportedConfigs = map[string]SSV{
	MainnetSSV.Name:      MainnetSSV,
	HoleskySSV.Name:      HoleskySSV,
	HoleskyStageSSV.Name: HoleskyStageSSV,
	LocalTestnetSSV.Name: LocalTestnetSSV,
	HoleskyE2ESSV.Name:   HoleskyE2ESSV,
}

const alanForkName = "alan"

func GetNetworkConfigByName(name string) (SSV, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return SSV{}, fmt.Errorf("network not supported: %v", name)
}

// DomainTypeProvider is an interface for getting the domain type based on the current or given epoch.
type DomainTypeProvider interface {
	DomainType() spectypes.DomainType
	NextDomainType() spectypes.DomainType
	DomainTypeAtEpoch(epoch phase0.Epoch) spectypes.DomainType
}

type BeaconNetwork interface {
	GenesisForkVersion() phase0.Version
	MinGenesisTime() time.Time
	SlotDuration() time.Duration
	SlotsPerEpoch() phase0.Slot
	EstimatedCurrentSlot() phase0.Slot
	EstimatedSlotAtTime(time time.Time) phase0.Slot
	EstimatedTimeAtSlot(slot phase0.Slot) time.Time
	EstimatedCurrentEpoch() phase0.Epoch
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot
	EpochStartTime(epoch phase0.Epoch) time.Time

	GetSlotStartTime(slot phase0.Slot) time.Time
	GetSlotEndTime(slot phase0.Slot) time.Time
	IsFirstSlotOfEpoch(slot phase0.Slot) bool
	GetEpochFirstSlot(epoch phase0.Epoch) phase0.Slot

	EpochsPerSyncCommitteePeriod() phase0.Epoch
	EstimatedSyncCommitteePeriodAtEpoch(epoch phase0.Epoch) uint64
	FirstEpochOfSyncPeriod(period uint64) phase0.Epoch
	LastSlotOfSyncPeriod(period uint64) phase0.Slot
}

var _ BeaconNetwork = NetworkConfig{}

type NetworkConfig struct {
	SSV
	Beacon
}

func (n NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return fmt.Sprintf("<malformed: %v>", err)
	}

	return string(b)
}

func (n NetworkConfig) PastAlanFork() bool {
	return n.EstimatedCurrentEpoch() >= n.AlanForkEpoch
}

func (n NetworkConfig) PastAlanForkAtEpoch(epoch phase0.Epoch) bool {
	return epoch >= n.AlanForkEpoch
}

// GenesisForkVersion returns the genesis fork version of the network.
func (n NetworkConfig) GenesisForkVersion() phase0.Version {
	return n.Beacon.GenesisForkVersion
}

// SlotDuration returns slot duration
func (n NetworkConfig) SlotDuration() time.Duration {
	return n.Beacon.SlotDuration
}

// SlotsPerEpoch returns number of slots per one epoch
func (n NetworkConfig) SlotsPerEpoch() phase0.Slot {
	return n.Beacon.SlotsPerEpoch
}

// MinGenesisTime returns the min genesis time
func (n NetworkConfig) MinGenesisTime() time.Time {
	return n.Beacon.MinGenesisTime
}

func (n NetworkConfig) EpochsPerSyncCommitteePeriod() phase0.Epoch {
	return n.Beacon.EpochsPerSyncCommitteePeriod
}

// DomainType returns current domain type based on the current fork.
func (n NetworkConfig) DomainType() spectypes.DomainType {
	return n.DomainTypeAtEpoch(n.EstimatedCurrentEpoch())
}
