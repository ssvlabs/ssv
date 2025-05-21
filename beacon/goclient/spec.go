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
	DefaultSlotDuration  = 12 * time.Second
	DefaultSlotsPerEpoch = uint64(32)
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
		return networkconfig.BeaconConfig{}, fmt.Errorf("config name not known by chain")
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
			gc.log.Warn("seconds per slot not known by chain, using default value",
				zap.Any("value", slotDuration))
		}
	}

	slotsPerEpoch := DefaultSlotsPerEpoch
	if slotsPerEpochRaw, ok := specResponse["SLOTS_PER_EPOCH"]; ok {
		if slotsPerEpochDecoded, ok := slotsPerEpochRaw.(uint64); ok {
			slotsPerEpoch = slotsPerEpochDecoded
		} else {
			gc.log.Warn("slots per epoch not known by chain, using default value",
				zap.Uint64("value", slotsPerEpoch))
		}
	}

	genesisResponse, err := genesisForClient(ctx, gc.log, client)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Genesis"), zap.Error(err))
		return networkconfig.BeaconConfig{}, fmt.Errorf("failed to obtain genesis response: %w", err)
	}

	beaconConfig := networkconfig.BeaconConfig{
		BeaconName:    networkName,
		SlotDuration:  slotDuration,
		SlotsPerEpoch: slotsPerEpoch,
		ForkVersion:   genesisResponse.GenesisForkVersion,
		GenesisTime:   genesisResponse.GenesisTime,
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
