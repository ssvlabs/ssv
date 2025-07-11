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
	DefaultSlotsPerEpoch                        = uint64(32)
	DefaultEpochsPerSyncCommitteePeriod         = uint64(256)
	DefaultSyncCommitteeSize                    = uint64(512)
	DefaultSyncCommitteeSubnetCount             = uint64(4)
	DefaultTargetAggregatorsPerSyncSubcommittee = uint64(16)
	DefaultTargetAggregatorsPerCommittee        = uint64(16)
	DefaultIntervalsPerSlot                     = uint64(3)
)

// BeaconConfig returns the network Beacon configuration
func (gc *GoClient) BeaconConfig() *networkconfig.Beacon {
	// BeaconConfig must be called if GoClient is initialized (gc.beaconConfig is set)
	// It fails otherwise.
	config := gc.getBeaconConfig()
	return config
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
func (gc *GoClient) fetchBeaconConfig(ctx context.Context, client *eth2clienthttp.Service) (*networkconfig.Beacon, error) {
	specResponse, err := specForClient(ctx, gc.log, client)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Spec"), zap.Error(err))
		return nil, fmt.Errorf("failed to obtain spec response: %w", err)
	}

	// types of most values are already cast: https://github.com/attestantio/go-eth2-client/blob/v0.21.7/http/spec.go#L78

	networkName, err := get[string](specResponse, "CONFIG_NAME")
	if err != nil {
		return nil, fmt.Errorf("get CONFIG_NAME: %w", err)
	}

	slotDuration, err := get[time.Duration](specResponse, "SECONDS_PER_SLOT")
	if err != nil {
		gc.log.Warn("could not get extract parameter from beacon node response, using default value",
			zap.Error(err),
			zap.String("parameter", "SECONDS_PER_SLOT"),
			zap.Any("default_value", DefaultSlotDuration))

		slotDuration = DefaultSlotDuration
	}

	slotsPerEpoch, err := get[uint64](specResponse, "SLOTS_PER_EPOCH")
	if err != nil {
		gc.log.Warn("could not get extract parameter from beacon node response, using default value",
			zap.Error(err),
			zap.String("parameter", "SLOTS_PER_EPOCH"),
			zap.Any("default_value", DefaultSlotsPerEpoch))

		slotsPerEpoch = DefaultSlotsPerEpoch
	}

	epochsPerSyncCommitteePeriod, err := get[uint64](specResponse, "EPOCHS_PER_SYNC_COMMITTEE_PERIOD")
	if err != nil {
		gc.log.Warn("could not get extract parameter from beacon node response, using default value",
			zap.Error(err),
			zap.String("parameter", "EPOCHS_PER_SYNC_COMMITTEE_PERIOD"),
			zap.Any("default_value", DefaultEpochsPerSyncCommitteePeriod))

		epochsPerSyncCommitteePeriod = DefaultEpochsPerSyncCommitteePeriod
	}

	syncCommitteeSize, err := get[uint64](specResponse, "SYNC_COMMITTEE_SIZE")
	if err != nil {
		gc.log.Warn("could not get extract parameter from beacon node response, using default value",
			zap.Error(err),
			zap.String("parameter", "SYNC_COMMITTEE_SIZE"),
			zap.Any("default_value", DefaultSyncCommitteeSize))

		syncCommitteeSize = DefaultSyncCommitteeSize
	}

	targetAggregatorsPerCommittee, err := get[uint64](specResponse, "TARGET_AGGREGATORS_PER_COMMITTEE")
	if err != nil {
		gc.log.Warn("could not get extract parameter from beacon node response, using default value",
			zap.Error(err),
			zap.String("parameter", "TARGET_AGGREGATORS_PER_COMMITTEE"),
			zap.Any("default_value", DefaultTargetAggregatorsPerCommittee))

		targetAggregatorsPerCommittee = DefaultTargetAggregatorsPerCommittee
	}

	targetAggregatorsPerSyncSubcommittee, err := get[uint64](specResponse, "TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE")
	if err != nil {
		gc.log.Warn("could not get extract parameter from beacon node response, using default value",
			zap.Error(err),
			zap.String("parameter", "TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE"),
			zap.Any("default_value", DefaultTargetAggregatorsPerSyncSubcommittee))

		targetAggregatorsPerSyncSubcommittee = DefaultTargetAggregatorsPerSyncSubcommittee
	}

	intervalsPerSlot, err := get[uint64](specResponse, "INTERVALS_PER_SLOT")
	if err != nil {
		gc.log.Warn("could not get extract parameter from beacon node response, using default value",
			zap.Error(err),
			zap.String("parameter", "INTERVALS_PER_SLOT"),
			zap.Any("default_value", DefaultIntervalsPerSlot))

		intervalsPerSlot = DefaultIntervalsPerSlot
	}

	syncCommitteeSubnetCount, err := get[uint64](specResponse, "SYNC_COMMITTEE_SUBNET_COUNT")
	if err != nil {
		gc.log.Warn("could not get extract parameter from beacon node response, using default value",
			zap.Error(err),
			zap.String("parameter", "SYNC_COMMITTEE_SUBNET_COUNT"),
			zap.Any("default_value", DefaultSyncCommitteeSubnetCount))

		syncCommitteeSubnetCount = DefaultSyncCommitteeSubnetCount
	}

	forkData, err := gc.getForkData(specResponse)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Spec"), zap.Error(err))
		return nil, fmt.Errorf("failed to extract fork data: %w", err)
	}

	genesisResponse, err := genesisForClient(ctx, gc.log, client)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Genesis"), zap.Error(err))
		return nil, fmt.Errorf("failed to obtain genesis response: %w", err)
	}

	beaconConfig := &networkconfig.Beacon{
		BeaconNetwork:                        networkName,
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
		BeaconForks:                          forkData,
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

func get[T any](response map[string]any, key string) (T, error) {
	var zero T

	val, ok := response[key]
	if !ok {
		return zero, fmt.Errorf("missing key '%s' in response", key)
	}

	switch t := val.(type) {
	case T:
		return t, nil
	default:
		return zero, fmt.Errorf("key %s of type '%T' cannot be converted to '%T'", key, val, zero)
	}
}
