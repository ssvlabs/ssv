package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/attestantio/go-eth2-client/http"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/auto"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/networkconfig"
	"golang.org/x/exp/maps"

	//eth2client "github.com/attestantio/go-eth2-client/http"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/rs/zerolog"

	"go.uber.org/zap"

	beaconproxy "github.com/bloxapp/ssv/e2e/beacon_proxy"
	"github.com/bloxapp/ssv/e2e/beacon_proxy/intercept/slashinginterceptor"
)

type BeaconProxyCmd struct {
	BeaconNodeUrl string   `required:"" env:"BEACON_NODE_URL" help:"URL for the Beacon node to proxy and intercept."`
	Gateways      []string `required:"" env:"GATEWAYS"        help:"Names of the gateways to provide."`
	BasePort      int      `            env:"BASE_PORT"       help:"Base port for the gateways."                     default:"6631"`
}

type BeaconProxyJSON struct {
	Validators map[phase0.ValidatorIndex]string `json:"beacon_proxy"`
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

func BeaconClientConnection(pctx context.Context, beaconUrl string) error {
	ctx, c := context.WithCancel(pctx)
	defer c()
	client, err := http.New(ctx,
		// WithAddress supplies the address of the beacon node, as a URL.
		http.WithAddress(beaconUrl),
		// LogLevel supplies the level of logging to carry out.
		http.WithLogLevel(zerolog.WarnLevel),
	)
	if err != nil {
		return err
	}
	_, err = client.(eth2client.GenesisProvider).Genesis(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (cmd *BeaconProxyCmd) Run(logger *zap.Logger, globals Globals) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := BeaconClientConnection(ctx, cmd.BeaconNodeUrl); err != nil {
		return err
	}
	logger.Info("Beacon client status OK")

	contents, err := os.ReadFile(globals.ValidatorsFile)
	if err != nil {
		return fmt.Errorf("failed to read file contents: %s, %w", globals.ValidatorsFile, err)
	}

	var beaconProxyJSON BeaconProxyJSON // dx => tests
	if err = json.Unmarshal(contents, &beaconProxyJSON); err != nil {
		return fmt.Errorf("error parsing json file: %s, %w", globals.ValidatorsFile, err)
	}

	validatorsData, err := GetValidators(ctx, cmd.BeaconNodeUrl, maps.Keys(beaconProxyJSON.Validators))
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

	// TODO: implement ability to select what test to run
	//interceptor := intercept.NewHappyInterceptor(maps.Values(validatorsData))
	//var validatorsRunningSlashing []*v1.Validator
	//for _, valData := range validatorsData {
	//	if validators[valData.Index] == "slashing" {
	//		validatorsRunningSlashing = append(validatorsRunningSlashing, valData)
	//	}
	//}

	networkCfg := networkconfig.HoleskyE2E

	const startEpochDelay = 1 // TODO: change to 2 after debugging is done
	startEpoch := networkCfg.Beacon.EstimatedCurrentEpoch() + startEpochDelay

	interceptor := slashinginterceptor.New(logger, networkCfg.Beacon.GetNetwork(), startEpoch, true, maps.Values(validatorsData))
	go interceptor.WatchSubmissions()

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
