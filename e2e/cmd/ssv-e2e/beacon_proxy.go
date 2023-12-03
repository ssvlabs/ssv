package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	beaconproxy "github.com/bloxapp/ssv/e2e/beacon_proxy"
	"github.com/bloxapp/ssv/e2e/beacon_proxy/intercept"
	"go.uber.org/zap"
)

type BeaconProxyCmd struct {
	BeaconNodeUrl string   `required:"" env:"BEACON_NODE_URL" help:"URL for the Beacon node to proxy and intercept."`
	Gateways      []string `required:"" env:"GATEWAYS"        help:"Names of the gateways to provide."`
	BasePort      int      `            env:"BASE_PORT"       help:"Base port for the gateways."                     default:"6631"`
}

func (cmd *BeaconProxyCmd) Run(logger *zap.Logger, globals Globals) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var validators map[string]string
	contents, err := os.ReadFile(globals.ValidatorsFile)
	if err != nil {
		return fmt.Errorf("failed to read file contents: %s, %w", globals.ValidatorsFile, err)
	}
	err = json.Unmarshal(contents, &validators)
	if err != nil {
		return fmt.Errorf("error parsing json file: %s, %w", globals.ValidatorsFile, err)
	}

	gateways := make([]beaconproxy.Gateway, len(cmd.Gateways))
	interceptor := intercept.NewHappyInterceptor(nil) // todo pass validators form validators.json
	for i, gw := range cmd.Gateways {
		gateways[i] = beaconproxy.Gateway{
			Name:        gw,
			Port:        cmd.BasePort + i,
			Interceptor: interceptor,
		}
	}

	proxy, err := beaconproxy.New(ctx, logger, cmd.BeaconNodeUrl, gateways, validators)
	if err != nil {
		return fmt.Errorf("beacon proxy creation error: %w", err)
	}
	return proxy.Run(ctx)
}
