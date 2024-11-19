package goclient

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	networkconfig "github.com/ssvlabs/ssv/network/config"
)

// BeaconConfig must be called if GoClient is initialized (gc.beaconConfig is set)
func (gc *GoClient) BeaconConfig() networkconfig.Beacon {
	if gc.beaconConfig == nil {
		gc.log.Fatal("BeaconConfig must be called after GoClient is initialized (gc.beaconConfig is set)")
	}
	return *gc.beaconConfig
}

// fetchBeaconConfig must be called once on GoClient's initialization
func (gc *GoClient) fetchBeaconConfig() (*networkconfig.Beacon, error) {
	specResponse, err := gc.client.Spec(gc.ctx, &api.SpecOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain spec response: %w", err)
	}
	if specResponse == nil {
		return nil, fmt.Errorf("spec response is nil")
	}
	if specResponse.Data == nil {
		return nil, fmt.Errorf("spec response data is nil")
	}

	// types of most values are already cast: https://github.com/attestantio/go-eth2-client/blob/v0.21.7/http/spec.go#L78

	configNameRaw, ok := specResponse.Data["CONFIG_NAME"]
	if !ok {
		return nil, fmt.Errorf("config name not known by chain")
	}

	configName, ok := configNameRaw.(string)
	if !ok {
		return nil, fmt.Errorf("failed to decode config name")
	}

	genesisForkVersionRaw, ok := specResponse.Data["GENESIS_FORK_VERSION"]
	if !ok {
		return nil, fmt.Errorf("genesis fork version not known by chain")
	}

	genesisForkVersion, ok := genesisForkVersionRaw.(phase0.Version)
	if !ok {
		return nil, fmt.Errorf("failed to decode genesis fork version")
	}

	capellaForkVersionRaw, ok := specResponse.Data["CAPELLA_FORK_VERSION"]
	if !ok {
		return nil, fmt.Errorf("capella fork version not known by chain")
	}

	capellaForkVersion, ok := capellaForkVersionRaw.(phase0.Version)
	if !ok {
		return nil, fmt.Errorf("failed to decode capella fork version")
	}

	minGenesisTimeRaw, ok := specResponse.Data["MIN_GENESIS_TIME"]
	if !ok {
		return nil, fmt.Errorf("min genesis time not known by chain")
	}

	minGenesisTime, ok := minGenesisTimeRaw.(time.Time)
	if !ok {
		return nil, fmt.Errorf("failed to decode min genesis time")
	}

	slotDurationRaw, ok := specResponse.Data["SECONDS_PER_SLOT"]
	if !ok {
		return nil, fmt.Errorf("slot duration not known by chain")
	}

	slotDuration, ok := slotDurationRaw.(time.Duration)
	if !ok {
		return nil, fmt.Errorf("failed to decode slot duration")
	}

	slotsPerEpochRaw, ok := specResponse.Data["SLOTS_PER_EPOCH"]
	if !ok {
		return nil, fmt.Errorf("slots per epoch not known by chain")
	}

	slotsPerEpoch, ok := slotsPerEpochRaw.(uint64)
	if !ok {
		return nil, fmt.Errorf("failed to decode slots per epoch")
	}

	epochsPerSyncCommitteePeriodRaw, ok := specResponse.Data["EPOCHS_PER_SYNC_COMMITTEE_PERIOD"]
	if !ok {
		return nil, fmt.Errorf("epochs per sync committee not known by chain")
	}

	epochsPerSyncCommittee, ok := epochsPerSyncCommitteePeriodRaw.(uint64)
	if !ok {
		return nil, fmt.Errorf("failed to decode epochs per sync committee")
	}

	syncCommitteeSizeRaw, ok := specResponse.Data["SYNC_COMMITTEE_SIZE"]
	if !ok {
		return nil, fmt.Errorf("sync committee size not known by chain")
	}

	syncCommitteeSize, ok := syncCommitteeSizeRaw.(uint64)
	if !ok {
		return nil, fmt.Errorf("failed to decode sync committee size")
	}

	syncCommitteeSubnetCountRaw, ok := specResponse.Data["SYNC_COMMITTEE_SUBNET_COUNT"]
	if !ok {
		return nil, fmt.Errorf("sync committee subnet count not known by chain")
	}

	syncCommitteeSubnetCount, ok := syncCommitteeSubnetCountRaw.(uint64)
	if !ok {
		return nil, fmt.Errorf("failed to decode sync committee subnet count")
	}

	targetAggregatorsPerCommitteeRaw, ok := specResponse.Data["TARGET_AGGREGATORS_PER_COMMITTEE"]
	if !ok {
		return nil, fmt.Errorf("target aggregators per committee not known by chain")
	}

	targetAggregatorsPerCommittee, ok := targetAggregatorsPerCommitteeRaw.(uint64)
	if !ok {
		return nil, fmt.Errorf("failed to decode target aggregators per committee")
	}

	targetAggregatorsPerSyncSubcommitteeRaw, ok := specResponse.Data["TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE"]
	if !ok {
		return nil, fmt.Errorf("target aggregators per sync subcommittee not known by chain")
	}

	targetAggregatorsPerSyncSubcommittee, ok := targetAggregatorsPerSyncSubcommitteeRaw.(uint64)
	if !ok {
		return nil, fmt.Errorf("failed to decode target aggregators per sync subcommittee")
	}

	intervalsPerSlotRaw, ok := specResponse.Data["INTERVALS_PER_SLOT"]
	if !ok {
		return nil, fmt.Errorf("intervals per slots not known by chain")
	}

	intervalsPerSlot, ok := intervalsPerSlotRaw.(uint64)
	if !ok {
		return nil, fmt.Errorf("failed to decode intervals per slots")
	}

	return &networkconfig.Beacon{
		ConfigName:                           configName,
		GenesisForkVersion:                   genesisForkVersion,
		CapellaForkVersion:                   capellaForkVersion,
		MinGenesisTime:                       minGenesisTime,
		SlotDuration:                         slotDuration,
		SlotsPerEpoch:                        phase0.Slot(slotsPerEpoch),
		EpochsPerSyncCommitteePeriod:         phase0.Epoch(epochsPerSyncCommittee),
		SyncCommitteeSize:                    syncCommitteeSize,
		SyncCommitteeSubnetCount:             syncCommitteeSubnetCount,
		TargetAggregatorsPerSyncSubcommittee: targetAggregatorsPerSyncSubcommittee,
		TargetAggregatorsPerCommittee:        targetAggregatorsPerCommittee,
		IntervalsPerSlot:                     intervalsPerSlot,
	}, nil
}
