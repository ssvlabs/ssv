package goclient

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/networkconfig"
)

// BeaconConfig must be called if GoClient is initialized (gc.beaconConfig is set)
func (gc *GoClient) BeaconConfig() networkconfig.BeaconConfig {
	if gc.beaconConfig == nil {
		gc.log.Fatal("BeaconConfig must be called after GoClient is initialized (gc.beaconConfig is set)")
	}
	return *gc.beaconConfig
}

// fetchBeaconConfig must be called once on GoClient's initialization
func (gc *GoClient) fetchBeaconConfig() (*networkconfig.BeaconConfig, error) {
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

	return &networkconfig.BeaconConfig{
		GenesisForkVersionVal:           genesisForkVersion,
		CapellaForkVersionVal:           capellaForkVersion,
		MinGenesisTimeVal:               minGenesisTime,
		SlotDurationVal:                 slotDuration,
		SlotsPerEpochVal:                phase0.Slot(slotsPerEpoch),
		EpochsPerSyncCommitteePeriodVal: phase0.Epoch(epochsPerSyncCommittee),
	}, nil
}
