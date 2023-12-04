package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/auto"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"golang.org/x/exp/maps"

	//eth2client "github.com/attestantio/go-eth2-client/http"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/rs/zerolog"

	"go.uber.org/zap"

	beaconproxy "github.com/bloxapp/ssv/e2e/beacon_proxy"
	"github.com/bloxapp/ssv/e2e/beacon_proxy/intercept"
)

type BeaconProxyCmd struct {
	BeaconNodeUrl string   `required:"" env:"BEACON_NODE_URL" help:"URL for the Beacon node to proxy and intercept."`
	Gateways      []string `required:"" env:"GATEWAYS"        help:"Names of the gateways to provide."`
	BasePort      int      `            env:"BASE_PORT"       help:"Base port for the gateways."                     default:"6631"`
}

func GetValidators(ctx context.Context, beaconURL string, idxs []phase0.ValidatorIndex) (map[phase0.ValidatorIndex]*v1.Validator, error) {
	// todo: maybe create the client on top and pass down to all components
	client, err := auto.New(
		ctx,
		auto.WithAddress(beaconURL),
		auto.WithTimeout(30*time.Second),
		auto.WithLogLevel(zerolog.ErrorLevel),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to connect to remote beacon node: %w", err)
	}

	vmap, err := client.(eth2client.ValidatorsProvider).Validators(ctx, "head", idxs)
	if err != nil {
		return nil, err
	}
	return vmap, nil
}

func (cmd *BeaconProxyCmd) Run(logger *zap.Logger, globals Globals) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var validators map[phase0.ValidatorIndex]string // dx => tests
	contents, err := os.ReadFile(globals.ValidatorsFile)
	if err != nil {
		return fmt.Errorf("failed to read file contents: %s, %w", globals.ValidatorsFile, err)
	}
	err = json.Unmarshal(contents, &validators)
	if err != nil {
		return fmt.Errorf("error parsing json file: %s, %w", globals.ValidatorsFile, err)
	}

	validatorsData, err := GetValidators(ctx, cmd.BeaconNodeUrl, maps.Keys(validators))
	if err != nil {
		return fmt.Errorf("failed to get validators data from beacon node err:%v", err)
	}

	for idx, v := range validatorsData {
		if v.Status != v1.ValidatorStateActiveOngoing || v.Validator.Slashed {
			logger.Fatal("Validator is not active", zap.Uint64("id", uint64(idx)), zap.String("status", v.Status.String()))
		}
	}

	logger.Info("Got all validators data", zap.Int("count", len(validatorsData)))

	gateways := make([]beaconproxy.Gateway, len(cmd.Gateways))
	// TODO: call NewSlashingInterceptor with startEpoch=currentEpoch+2 and pass the validator objects from Beacon node
	// to the interceptor
	interceptor := intercept.NewHappyInterceptor(maps.Values(validatorsData))
	for i, gw := range cmd.Gateways {
		gateways[i] = beaconproxy.Gateway{
			Name:        gw,
			Port:        cmd.BasePort + i,
			Interceptor: interceptor,
		}
	}

	proxy, err := beaconproxy.New(ctx, logger, cmd.BeaconNodeUrl, gateways)
	if err != nil {
		return fmt.Errorf("beacon proxy creation error: %w", err)
	}
	return proxy.Run(ctx)
}
