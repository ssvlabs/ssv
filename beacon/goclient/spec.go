package goclient

import (
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
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
	return *gc.beaconConfig
}

// fetchBeaconConfig must be called once on GoClient's initialization
func (gc *GoClient) fetchBeaconConfig() (*networkconfig.Beacon, error) {
	start := time.Now()
	specResponse, err := gc.multiClient.Spec(gc.ctx, &api.SpecOpts{})
	recordRequestDuration(gc.ctx, "Spec", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Spec"), zap.Error(err))
		return nil, fmt.Errorf("failed to obtain spec response: %w", err)
	}
	if specResponse == nil {
		gc.log.Error(clNilResponseErrMsg, zap.String("api", "Spec"))
		return nil, fmt.Errorf("spec response is nil")
	}
	if specResponse.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg, zap.String("api", "Spec"))
		return nil, fmt.Errorf("spec response data is nil")
	}

	// types of most values are already cast: https://github.com/attestantio/go-eth2-client/blob/v0.21.7/http/spec.go#L78

	networkNameRaw, ok := specResponse.Data["CONFIG_NAME"]
	if !ok {
		return nil, fmt.Errorf("config name not known by chain")
	}

	networkName, ok := networkNameRaw.(string)
	if !ok {
		return nil, fmt.Errorf("failed to decode config name")
	}

	capellaForkVersionRaw, ok := specResponse.Data["CAPELLA_FORK_VERSION"]
	if !ok {
		return nil, fmt.Errorf("capella fork version not known by chain")
	}

	capellaForkVersion, ok := capellaForkVersionRaw.(phase0.Version)
	if !ok {
		return nil, fmt.Errorf("failed to decode capella fork version")
	}

	slotDuration := DefaultSlotDuration
	if slotDurationRaw, ok := specResponse.Data["SECONDS_PER_SLOT"]; ok {
		if slotDurationDecoded, ok := slotDurationRaw.(time.Duration); ok {
			slotDuration = slotDurationDecoded
		} else {
			gc.log.Warn("seconds per slot not known by chain, using default value",
				zap.Any("value", slotDuration))
		}
	}

	slotsPerEpoch := DefaultSlotsPerEpoch
	if slotsPerEpochRaw, ok := specResponse.Data["SLOTS_PER_EPOCH"]; ok {
		if slotsPerEpochDecoded, ok := slotsPerEpochRaw.(uint64); ok {
			slotsPerEpoch = phase0.Slot(slotsPerEpochDecoded)
		} else {
			gc.log.Warn("slots per epoch not known by chain, using default value",
				zap.Any("value", slotsPerEpoch))
		}
	}

	epochsPerSyncCommitteePeriod := DefaultEpochsPerSyncCommitteePeriod
	if epochsPerSyncCommitteePeriodRaw, ok := specResponse.Data["EPOCHS_PER_SYNC_COMMITTEE_PERIOD"]; ok {
		if epochsPerSyncCommitteePeriodDecoded, ok := epochsPerSyncCommitteePeriodRaw.(uint64); ok {
			epochsPerSyncCommitteePeriod = phase0.Epoch(epochsPerSyncCommitteePeriodDecoded)
		} else {
			gc.log.Warn("epochs per sync committee not known by chain, using default value",
				zap.Any("value", epochsPerSyncCommitteePeriod))
		}
	}

	syncCommitteeSize := DefaultSyncCommitteeSize
	if syncCommitteeSizeRaw, ok := specResponse.Data["SYNC_COMMITTEE_SIZE"]; ok {
		if syncCommitteeSizeDecoded, ok := syncCommitteeSizeRaw.(uint64); ok {
			syncCommitteeSize = syncCommitteeSizeDecoded
		} else {
			gc.log.Warn("sync committee size not known by chain, using default value",
				zap.Any("value", syncCommitteeSize))
		}
	}

	syncCommitteeSubnetCount := DefaultSyncCommitteeSubnetCount
	if syncCommitteeSubnetCountRaw, ok := specResponse.Data["SYNC_COMMITTEE_SUBNET_COUNT"]; ok {
		if syncCommitteeSubnetCountDecoded, ok := syncCommitteeSubnetCountRaw.(uint64); ok {
			syncCommitteeSubnetCount = syncCommitteeSubnetCountDecoded
		} else {
			gc.log.Warn("sync committee subnet count not known by chain, using default value",
				zap.Any("value", syncCommitteeSubnetCount))
		}
	}

	targetAggregatorsPerCommittee := DefaultTargetAggregatorsPerCommittee
	if targetAggregatorsPerCommitteeRaw, ok := specResponse.Data["TARGET_AGGREGATORS_PER_COMMITTEE"]; ok {
		if targetAggregatorsPerCommitteeDecoded, ok := targetAggregatorsPerCommitteeRaw.(uint64); ok {
			targetAggregatorsPerCommittee = targetAggregatorsPerCommitteeDecoded
		} else {
			gc.log.Warn("target aggregators per committee not known by chain, using default value",
				zap.Any("value", targetAggregatorsPerCommittee))
		}
	}

	targetAggregatorsPerSyncSubcommittee := DefaultTargetAggregatorsPerSyncSubcommittee
	if targetAggregatorsPerSyncSubcommitteeRaw, ok := specResponse.Data["TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE"]; ok {
		if targetAggregatorsPerSyncSubcommitteeDecoded, ok := targetAggregatorsPerSyncSubcommitteeRaw.(uint64); ok {
			targetAggregatorsPerSyncSubcommittee = targetAggregatorsPerSyncSubcommitteeDecoded
		} else {
			gc.log.Warn("target aggregators per sync subcommittee not known by chain, using default value",
				zap.Any("value", targetAggregatorsPerSyncSubcommittee))
		}
	}

	intervalsPerSlot := DefaultIntervalsPerSlot
	if intervalsPerSlotRaw, ok := specResponse.Data["INTERVALS_PER_SLOT"]; ok {
		if intervalsPerSlotDecoded, ok := intervalsPerSlotRaw.(uint64); ok {
			intervalsPerSlot = intervalsPerSlotDecoded
		} else {
			gc.log.Warn("intervals per slot not known by chain, using default value",
				zap.Any("value", intervalsPerSlot))
		}
	}

	start = time.Now()
	genesisResponse, err := gc.multiClient.Genesis(gc.ctx, &api.GenesisOpts{})
	recordRequestDuration(gc.ctx, "Genesis", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Genesis"), zap.Error(err))
		return nil, fmt.Errorf("failed to obtain genesis response: %w", err)
	}
	if genesisResponse == nil {
		gc.log.Error(clNilResponseErrMsg, zap.String("api", "Genesis"))
		return nil, fmt.Errorf("genesis response is nil")
	}
	if genesisResponse.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg, zap.String("api", "Genesis"))
		return nil, fmt.Errorf("genesis response data is nil")
	}

	return &networkconfig.Beacon{
		NetworkName:                          networkName,
		CapellaForkVersion:                   capellaForkVersion,
		SlotDuration:                         slotDuration,
		SlotsPerEpoch:                        slotsPerEpoch,
		EpochsPerSyncCommitteePeriod:         epochsPerSyncCommitteePeriod,
		SyncCommitteeSize:                    syncCommitteeSize,
		SyncCommitteeSubnetCount:             syncCommitteeSubnetCount,
		TargetAggregatorsPerSyncSubcommittee: targetAggregatorsPerSyncSubcommittee,
		TargetAggregatorsPerCommittee:        targetAggregatorsPerCommittee,
		IntervalsPerSlot:                     intervalsPerSlot,
		Genesis:                              *genesisResponse.Data,
	}, nil
}
