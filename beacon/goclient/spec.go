package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec"
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

// BeaconConfig returns the network Beacon configuration
func (gc *GoClient) BeaconConfig() networkconfig.BeaconConfig {
	// BeaconConfig must be called if GoClient is initialized (gc.beaconConfig is set)
	// It fails otherwise.
	config := gc.getBeaconConfig()
	return *config
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

// fetchBeaconConfig must be called once on GoClient's initialization
func (gc *GoClient) fetchBeaconConfig(client *eth2clienthttp.Service) (networkconfig.BeaconConfig, error) {
	specResponse, err := specForClient(gc.ctx, gc.log, client)
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
			slotsPerEpoch = phase0.Slot(slotsPerEpochDecoded)
		} else {
			gc.log.Warn("slots per epoch wasn't found in beacon node response, using default value",
				zap.Any("value", slotsPerEpoch))
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

	forkData, err := gc.getForkData(specResponse)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Spec"), zap.Error(err))
		return networkconfig.BeaconConfig{}, fmt.Errorf("failed to extract fork data: %w", err)
	}

	genesisResponse, err := genesisForClient(gc.ctx, gc.log, client)
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
		GenesisForkVersion:                   genesisResponse.GenesisForkVersion,
		GenesisTime:                          genesisResponse.GenesisTime,
		GenesisValidatorsRoot:                genesisResponse.GenesisValidatorsRoot,
		Forks:                                forkData,
	}

	return beaconConfig, nil
}

func (gc *GoClient) getForkData(specResponse map[string]any) (map[spec.DataVersion]phase0.Fork, error) {
	if specResponse == nil {
		return nil, fmt.Errorf("spec response is nil")
	}

	getForkEpoch := func(key string, required bool) (phase0.Epoch, error) {
		raw, ok := specResponse[key]
		if !ok {
			if required {
				return 0, fmt.Errorf("%s is not known by chain", key)
			}
			return FarFutureEpoch, nil
		}
		forkVal, ok := raw.(uint64)
		if !ok {
			return 0, fmt.Errorf("failed to decode %s", key)
		}
		return phase0.Epoch(forkVal), nil
	}

	getForkVersion := func(key string) (phase0.Version, error) {
		raw, ok := specResponse[key]
		if !ok {
			return phase0.Version{}, fmt.Errorf("%s is not known by chain", key)
		}
		versionVal, ok := raw.(phase0.Version)
		if !ok {
			return phase0.Version{}, fmt.Errorf("failed to decode %s", key)
		}
		return versionVal, nil
	}

	genesisForkVersion, err := getForkVersion("GENESIS_FORK_VERSION")
	if err != nil {
		return nil, err
	}
	altairEpoch, err := getForkEpoch("ALTAIR_FORK_EPOCH", true)
	if err != nil {
		return nil, err
	}
	altairForkVersion, err := getForkVersion("ALTAIR_FORK_VERSION")
	if err != nil {
		return nil, err
	}
	bellatrixEpoch, err := getForkEpoch("BELLATRIX_FORK_EPOCH", true)
	if err != nil {
		return nil, err
	}
	bellatrixForkVersion, err := getForkVersion("BELLATRIX_FORK_VERSION")
	if err != nil {
		return nil, err
	}
	capellaEpoch, err := getForkEpoch("CAPELLA_FORK_EPOCH", true)
	if err != nil {
		return nil, err
	}
	capellaForkVersion, err := getForkVersion("CAPELLA_FORK_VERSION")
	if err != nil {
		return nil, err
	}
	denebEpoch, err := getForkEpoch("DENEB_FORK_EPOCH", true)
	if err != nil {
		return nil, err
	}
	denebForkVersion, err := getForkVersion("DENEB_FORK_VERSION")
	if err != nil {
		return nil, err
	}
	// After Electra fork happens on all networks,
	// - Electra check should become required
	// - Fulu check might be added as non-required
	electraEpoch, err := getForkEpoch("ELECTRA_FORK_EPOCH", false)
	if err != nil {
		return nil, err
	}
	electraForkVersion, err := getForkVersion("ELECTRA_FORK_VERSION")
	if err != nil {
		return nil, err
	}

	forkEpochs := map[spec.DataVersion]phase0.Fork{
		spec.DataVersionPhase0: {
			PreviousVersion: genesisForkVersion,
			CurrentVersion:  genesisForkVersion,
			Epoch:           0,
		},
		spec.DataVersionAltair: {
			PreviousVersion: genesisForkVersion,
			CurrentVersion:  altairForkVersion,
			Epoch:           altairEpoch,
		},
		spec.DataVersionBellatrix: {
			PreviousVersion: altairForkVersion,
			CurrentVersion:  bellatrixForkVersion,
			Epoch:           bellatrixEpoch,
		},
		spec.DataVersionCapella: {
			PreviousVersion: bellatrixForkVersion,
			CurrentVersion:  capellaForkVersion,
			Epoch:           capellaEpoch,
		},
		spec.DataVersionDeneb: {
			PreviousVersion: capellaForkVersion,
			CurrentVersion:  denebForkVersion,
			Epoch:           denebEpoch,
		},
		spec.DataVersionElectra: {
			PreviousVersion: denebForkVersion,
			CurrentVersion:  electraForkVersion,
			Epoch:           electraEpoch,
		},
	}

	return forkEpochs, nil
}
