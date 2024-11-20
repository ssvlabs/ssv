package goclient

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	networkconfig "github.com/ssvlabs/ssv/network/config"
)

const (
	DefaultSlotDuration                         = 12 * time.Second
	DefaultSlotsPerEpoch                        = phase0.Slot(32)
	DefaultEpochsPerSyncCommitteePeriod         = phase0.Epoch(256)
	DefaultSyncCommitteeSize                    = uint64(512)
	DefaultSyncCommitteeSubnetCount             = uint64(4)
	DefaultTargetAggregatorsPerSyncSubcommittee = uint64(16)
	DefaultTargetAggregatorsPerCommittee        = uint64(16)
	DefaultIntervalsPerSlot                     = uint64(3)
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

	genesisDelayRaw, ok := specResponse.Data["GENESIS_DELAY"]
	if !ok {
		return nil, fmt.Errorf("genesis delay not known by chain")
	}

	genesisDelay, ok := genesisDelayRaw.(time.Duration)
	if !ok {
		return nil, fmt.Errorf("failed to decode genesis delay")
	}

	slotDuration := DefaultSlotDuration
	if slotDurationRaw, ok := specResponse.Data["SECONDS_PER_SLOT"]; ok {
		if slotDurationDecoded, ok := slotDurationRaw.(time.Duration); ok {
			slotDuration = slotDurationDecoded
			gc.log.Warn("seconds per slot not known by chain, using default value",
				zap.Any("value", slotDuration))
		}
	}

	slotsPerEpoch := DefaultSlotsPerEpoch
	if slotsPerEpochRaw, ok := specResponse.Data["SLOTS_PER_EPOCH"]; ok {
		if slotsPerEpochDecoded, ok := slotsPerEpochRaw.(uint64); ok {
			slotsPerEpoch = phase0.Slot(slotsPerEpochDecoded)
			gc.log.Warn("slots per epoch not known by chain, using default value",
				zap.Any("value", slotsPerEpoch))
		}
	}

	epochsPerSyncCommitteePeriod := DefaultEpochsPerSyncCommitteePeriod
	if epochsPerSyncCommitteePeriodRaw, ok := specResponse.Data["EPOCHS_PER_SYNC_COMMITTEE_PERIOD"]; ok {
		if epochsPerSyncCommitteePeriodDecoded, ok := epochsPerSyncCommitteePeriodRaw.(uint64); ok {
			epochsPerSyncCommitteePeriod = phase0.Epoch(epochsPerSyncCommitteePeriodDecoded)
			gc.log.Warn("epochs per sync committee not known by chain, using default value",
				zap.Any("value", epochsPerSyncCommitteePeriod))
		}
	}

	syncCommitteeSize := DefaultSyncCommitteeSize
	if syncCommitteeSizeRaw, ok := specResponse.Data["SYNC_COMMITTEE_SIZE"]; ok {
		if syncCommitteeSizeDecoded, ok := syncCommitteeSizeRaw.(uint64); ok {
			syncCommitteeSize = syncCommitteeSizeDecoded
			gc.log.Warn("sync committee size not known by chain, using default value",
				zap.Any("value", syncCommitteeSize))
		}
	}

	syncCommitteeSubnetCount := DefaultSyncCommitteeSubnetCount
	if syncCommitteeSubnetCountRaw, ok := specResponse.Data["SYNC_COMMITTEE_SUBNET_COUNT"]; ok {
		if syncCommitteeSubnetCountDecoded, ok := syncCommitteeSubnetCountRaw.(uint64); ok {
			syncCommitteeSubnetCount = syncCommitteeSubnetCountDecoded
			gc.log.Warn("sync committee subnet count not known by chain, using default value",
				zap.Any("value", syncCommitteeSubnetCount))
		}
	}

	targetAggregatorsPerCommittee := DefaultTargetAggregatorsPerCommittee
	if targetAggregatorsPerCommitteeRaw, ok := specResponse.Data["TARGET_AGGREGATORS_PER_COMMITTEE"]; ok {
		if targetAggregatorsPerCommitteeDecoded, ok := targetAggregatorsPerCommitteeRaw.(uint64); ok {
			targetAggregatorsPerCommittee = targetAggregatorsPerCommitteeDecoded
			gc.log.Warn("target aggregators per committee not known by chain, using default value",
				zap.Any("value", targetAggregatorsPerCommittee))
		}
	}

	targetAggregatorsPerSyncSubcommittee := DefaultTargetAggregatorsPerSyncSubcommittee
	if targetAggregatorsPerSyncSubcommitteeRaw, ok := specResponse.Data["TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE"]; ok {
		if targetAggregatorsPerSyncSubcommitteeDecoded, ok := targetAggregatorsPerSyncSubcommitteeRaw.(uint64); ok {
			targetAggregatorsPerSyncSubcommittee = targetAggregatorsPerSyncSubcommitteeDecoded
			gc.log.Warn("target aggregators per sync subcommittee not known by chain, using default value",
				zap.Any("value", targetAggregatorsPerSyncSubcommittee))
		}
	}

	intervalsPerSlot := DefaultIntervalsPerSlot
	if intervalsPerSlotRaw, ok := specResponse.Data["INTERVALS_PER_SLOT"]; ok {
		if intervalsPerSlotDecoded, ok := intervalsPerSlotRaw.(uint64); ok {
			intervalsPerSlot = intervalsPerSlotDecoded
			gc.log.Warn("intervals per slot not known by chain, using default value",
				zap.Any("value", intervalsPerSlot))
		}
	}

	return &networkconfig.Beacon{
		ConfigName:                           configName,
		GenesisForkVersion:                   genesisForkVersion,
		CapellaForkVersion:                   capellaForkVersion,
		MinGenesisTime:                       minGenesisTime,
		GenesisDelay:                         genesisDelay,
		SlotDuration:                         slotDuration,
		SlotsPerEpoch:                        slotsPerEpoch,
		EpochsPerSyncCommitteePeriod:         epochsPerSyncCommitteePeriod,
		SyncCommitteeSize:                    syncCommitteeSize,
		SyncCommitteeSubnetCount:             syncCommitteeSubnetCount,
		TargetAggregatorsPerSyncSubcommittee: targetAggregatorsPerSyncSubcommittee,
		TargetAggregatorsPerCommittee:        targetAggregatorsPerCommittee,
		IntervalsPerSlot:                     intervalsPerSlot,
	}, nil
}
