package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	DefaultSlotDuration  = 12 * time.Second
	DefaultSlotsPerEpoch = phase0.Slot(32)
)

// BeaconConfig returns the network Beacon configuration
func (gc *GoClient) BeaconConfig() networkconfig.BeaconConfig {
	// BeaconConfig must be called if GoClient is initialized (gc.beaconConfig is set)
	// It fails otherwise.
	config := gc.getBeaconConfig()
	return *config
}

// fetchBeaconConfig must be called once on GoClient's initialization
func (gc *GoClient) fetchBeaconConfig(client *eth2clienthttp.Service) (networkconfig.BeaconConfig, error) {
	start := time.Now()
	specResponse, err := client.Spec(gc.ctx, &api.SpecOpts{})
	recordRequestDuration(gc.ctx, "Spec", client.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Spec"), zap.Error(err))
		return networkconfig.BeaconConfig{}, fmt.Errorf("failed to obtain spec response: %w", err)
	}
	if specResponse == nil {
		gc.log.Error(clNilResponseErrMsg, zap.String("api", "Spec"))
		return networkconfig.BeaconConfig{}, fmt.Errorf("spec response is nil")
	}
	if specResponse.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg, zap.String("api", "Spec"))
		return networkconfig.BeaconConfig{}, fmt.Errorf("spec response data is nil")
	}

	// types of most values are already cast: https://github.com/attestantio/go-eth2-client/blob/v0.21.7/http/spec.go#L78

	networkNameRaw, ok := specResponse.Data["CONFIG_NAME"]
	if !ok {
		return networkconfig.BeaconConfig{}, fmt.Errorf("config name not known by chain")
	}

	networkName, ok := networkNameRaw.(string)
	if !ok {
		return networkconfig.BeaconConfig{}, fmt.Errorf("failed to decode config name")
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

	start = time.Now()
	genesisResponse, err := client.Genesis(gc.ctx, &api.GenesisOpts{})
	recordRequestDuration(gc.ctx, "Genesis", client.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Genesis"), zap.Error(err))
		return networkconfig.BeaconConfig{}, fmt.Errorf("failed to obtain genesis response: %w", err)
	}
	if genesisResponse == nil {
		gc.log.Error(clNilResponseErrMsg, zap.String("api", "Genesis"))
		return networkconfig.BeaconConfig{}, fmt.Errorf("genesis response is nil")
	}
	if genesisResponse.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg, zap.String("api", "Genesis"))
		return networkconfig.BeaconConfig{}, fmt.Errorf("genesis response data is nil")
	}

	beaconConfig := networkconfig.BeaconConfig{
		BeaconName:    networkName,
		SlotDuration:  slotDuration,
		SlotsPerEpoch: slotsPerEpoch,
		ForkVersion:   genesisResponse.Data.GenesisForkVersion,
		GenesisTime:   genesisResponse.Data.GenesisTime,
	}

	return beaconConfig, nil
}
