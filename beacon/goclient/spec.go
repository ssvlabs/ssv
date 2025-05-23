package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	DefaultSlotDuration                         = 12 * time.Second
	DefaultSlotsPerEpoch                        = uint64(32)
	DefaultEpochsPerSyncCommitteePeriod         = phase0.Epoch(256)
	DefaultSyncCommitteeSize                    = uint64(512)
	DefaultSyncCommitteeSubnetCount             = uint64(4)
	DefaultTargetAggregatorsPerSyncSubcommittee = uint64(16)
	DefaultTargetAggregatorsPerCommittee        = uint64(16)
	DefaultIntervalsPerSlot                     = uint64(3)
)

// BeaconConfig returns the network Beacon configuration
func (gc *GoClient) BeaconConfig() networkconfig.BeaconConfig {
	// BeaconConfig must be called if GoClient is initialized (gc.beaconConfig is set)
	// It fails otherwise.
	config := gc.getBeaconConfig()
	return *config
}

// fetchBeaconConfig must be called once on GoClient's initialization
func (gc *GoClient) fetchBeaconConfig(ctx context.Context, client *eth2clienthttp.Service) (networkconfig.BeaconConfig, error) {
	specResponse, err := specForClient(ctx, gc.log, client)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Spec"), zap.Error(err))
		return networkconfig.BeaconConfig{}, fmt.Errorf("failed to obtain spec response: %w", err)
	}

	// types of most values are already cast: https://github.com/attestantio/go-eth2-client/blob/v0.21.7/http/spec.go#L78

	networkNameRaw, ok := specResponse["CONFIG_NAME"]
	if !ok {
		return networkconfig.BeaconConfig{}, fmt.Errorf("config name wasn't found in beacon node response")
	}

	networkName, ok := networkNameRaw.(string)
	if !ok {
		return networkconfig.BeaconConfig{}, fmt.Errorf("failed to decode config name")
	}

	slotDuration := DefaultSlotDuration
	if slotDurationRaw, ok := specResponse["SECONDS_PER_SLOT"]; ok {
		if slotDurationDecoded, ok := slotDurationRaw.(time.Duration); ok {
			slotDuration = slotDurationDecoded
		} else {
			gc.log.Warn("seconds per slot wasn't found in beacon node response, using default value",
				zap.Any("value", slotDuration))
		}
	}

	slotsPerEpoch := DefaultSlotsPerEpoch
	if slotsPerEpochRaw, ok := specResponse["SLOTS_PER_EPOCH"]; ok {
		if slotsPerEpochDecoded, ok := slotsPerEpochRaw.(uint64); ok {
			slotsPerEpoch = slotsPerEpochDecoded
		} else {
			gc.log.Warn("slots per epoch wasn't found in beacon node response, using default value",
				zap.Uint64("value", slotsPerEpoch))
		}
	}

	epochsPerSyncCommitteePeriod := DefaultEpochsPerSyncCommitteePeriod
	if epochsPerSyncCommitteePeriodRaw, ok := specResponse["EPOCHS_PER_SYNC_COMMITTEE_PERIOD"]; ok {
		if epochsPerSyncCommitteePeriodDecoded, ok := epochsPerSyncCommitteePeriodRaw.(uint64); ok {
			epochsPerSyncCommitteePeriod = phase0.Epoch(epochsPerSyncCommitteePeriodDecoded)
		} else {
			gc.log.Warn("epochs per sync committee wasn't found in beacon node response, using default value",
				zap.Any("value", epochsPerSyncCommitteePeriod))
		}
	}

	syncCommitteeSize := DefaultSyncCommitteeSize
	if syncCommitteeSizeRaw, ok := specResponse["SYNC_COMMITTEE_SIZE"]; ok {
		if syncCommitteeSizeDecoded, ok := syncCommitteeSizeRaw.(uint64); ok {
			syncCommitteeSize = syncCommitteeSizeDecoded
		} else {
			gc.log.Warn("sync committee size wasn't found in beacon node response, using default value",
				zap.Any("value", syncCommitteeSize))
		}
	}

	targetAggregatorsPerCommittee := DefaultTargetAggregatorsPerCommittee
	if targetAggregatorsPerCommitteeRaw, ok := specResponse["TARGET_AGGREGATORS_PER_COMMITTEE"]; ok {
		if targetAggregatorsPerCommitteeDecoded, ok := targetAggregatorsPerCommitteeRaw.(uint64); ok {
			targetAggregatorsPerCommittee = targetAggregatorsPerCommitteeDecoded
		} else {
			gc.log.Warn("target aggregators per committee wasn't found in beacon node response, using default value",
				zap.Any("value", targetAggregatorsPerCommittee))
		}
	}

	targetAggregatorsPerSyncSubcommittee := DefaultTargetAggregatorsPerSyncSubcommittee
	if targetAggregatorsPerSyncSubcommitteeRaw, ok := specResponse["TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE"]; ok {
		if targetAggregatorsPerSyncSubcommitteeDecoded, ok := targetAggregatorsPerSyncSubcommitteeRaw.(uint64); ok {
			targetAggregatorsPerSyncSubcommittee = targetAggregatorsPerSyncSubcommitteeDecoded
		} else {
			gc.log.Warn("target aggregators per sync subcommittee wasn't found in beacon node response, using default value",
				zap.Any("value", targetAggregatorsPerSyncSubcommittee))
		}
	}

	intervalsPerSlot := DefaultIntervalsPerSlot
	if intervalsPerSlotRaw, ok := specResponse["INTERVALS_PER_SLOT"]; ok {
		if intervalsPerSlotDecoded, ok := intervalsPerSlotRaw.(uint64); ok {
			intervalsPerSlot = intervalsPerSlotDecoded
		} else {
			gc.log.Warn("intervals per slot wasn't found in beacon node response, using default value",
				zap.Any("value", intervalsPerSlot))
		}
	}

	syncCommitteeSubnetCount := DefaultSyncCommitteeSubnetCount
	if syncCommitteeSubnetCountRaw, ok := specResponse["SYNC_COMMITTEE_SUBNET_COUNT"]; ok {
		if syncCommitteeSubnetCountDecoded, ok := syncCommitteeSubnetCountRaw.(uint64); ok {
			syncCommitteeSubnetCount = syncCommitteeSubnetCountDecoded
		} else {
			gc.log.Warn("sync committee subnet count wasn't found in beacon node response, using default value",
				zap.Any("value", syncCommitteeSubnetCount))
		}
	}

	genesisResponse, err := genesisForClient(ctx, gc.log, client)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Genesis"), zap.Error(err))
		return networkconfig.BeaconConfig{}, fmt.Errorf("failed to obtain genesis response: %w", err)
	}

	beaconConfig := networkconfig.BeaconConfig{
		BeaconName:                           networkName,
		SlotDuration:                         slotDuration,
		SlotsPerEpoch:                        slotsPerEpoch,
		EpochsPerSyncCommitteePeriod:         epochsPerSyncCommitteePeriod,
		SyncCommitteeSize:                    syncCommitteeSize,
		SyncCommitteeSubnetCount:             syncCommitteeSubnetCount,
		TargetAggregatorsPerSyncSubcommittee: targetAggregatorsPerSyncSubcommittee,
		TargetAggregatorsPerCommittee:        targetAggregatorsPerCommittee,
		IntervalsPerSlot:                     intervalsPerSlot,
		ForkVersion:                          genesisResponse.GenesisForkVersion,
		GenesisTime:                          genesisResponse.GenesisTime,
		GenesisValidatorsRoot:                genesisResponse.GenesisValidatorsRoot,
	}

	return beaconConfig, nil
}

func (gc *GoClient) Spec(ctx context.Context) (map[string]any, error) {
	return specForClient(ctx, gc.log, gc.multiClient)
}

func specForClient(ctx context.Context, log *zap.Logger, provider client.Service) (map[string]any, error) {
	start := time.Now()
	specResponse, err := provider.(client.SpecProvider).Spec(ctx, &api.SpecOpts{})
	recordRequestDuration(ctx, "Spec", provider.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		log.Error(clResponseErrMsg,
			zap.String("api", "Spec"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to obtain spec response: %w", err)
	}
	if specResponse == nil {
		log.Error(clNilResponseErrMsg,
			zap.String("api", "Spec"),
		)
		return nil, fmt.Errorf("spec response is nil")
	}
	if specResponse.Data == nil {
		log.Error(clNilResponseDataErrMsg,
			zap.String("api", "Spec"),
		)
		return nil, fmt.Errorf("spec response data is nil")
	}

	return specResponse.Data, nil
}
