package operator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

func run(ctx context.Context, cfg *config, logger *zap.Logger) error {
	// Validate the configuration up front, before any network I/O, so a misconfigured node
	// fails fast (e.g. an invalid ProposerDelay is rejected before the beacon client is built,
	// rather than after connecting to the beacon). resolveAndValidate only reads cfg.
	res, err := cfg.resolveAndValidate(logger)
	if err != nil {
		return err
	}

	ssvNetworkConfig, err := setupSSVNetwork(logger, cfg)
	if err != nil {
		return fmt.Errorf("could not setup network: %w", err)
	}

	logger.Info("connecting CL(s)",
		fields.Address(cfg.ConsensusClient.BeaconNodeAddr),
		zap.Bool("with_weighted_attestation_data", cfg.ConsensusClient.WithWeightedAttestationData),
		zap.Bool("with_parallel_submissions", cfg.ConsensusClient.WithParallelSubmissions),
	)

	cliopt, err := goclient.NewOptions(cfg.ConsensusClient, cfg.ProposerDelay)
	if err != nil {
		return startupError{
			err:    fmt.Errorf("failed to create beacon client options: %w", err),
			fields: []zap.Field{fields.Address(cfg.ConsensusClient.BeaconNodeAddr)},
		}
	}
	consensusClient, err := goclient.New(ctx, logger, cliopt)
	if err != nil {
		return startupError{
			err:    fmt.Errorf("failed to create beacon go-client: %w", err),
			fields: []zap.Field{fields.Address(cfg.ConsensusClient.BeaconNodeAddr)},
		}
	}

	networkConfig := &networkconfig.Network{
		SSV:    ssvNetworkConfig,
		Beacon: consensusClient.BeaconConfig(),
	}

	executionAddrList := strings.Split(cfg.ExecutionClient.Addr, ";")
	if len(executionAddrList) == 0 {
		return errors.New("no execution node address provided")
	}

	logger.Info("connecting EL(s)",
		fields.Addresses(executionAddrList),
		zap.Duration("request_timeout", cfg.ExecutionClient.ConnectionTimeout),
		zap.Uint64("sync_distance_tolerance", cfg.ExecutionClient.SyncDistanceTolerance),
	)

	var executionClient executionclient.Provider
	if len(executionAddrList) == 1 {
		ec, err := executionclient.New(
			ctx,
			executionAddrList[0],
			ssvNetworkConfig.RegistryContractAddr,
			executionclient.WithLogger(logger),
			executionclient.WithReqTimeout(cfg.ExecutionClient.ConnectionTimeout),
			executionclient.WithSyncDistanceTolerance(cfg.ExecutionClient.SyncDistanceTolerance),
		)
		if err != nil {
			return fmt.Errorf("could not connect to execution client: %w", err)
		}

		executionClient = ec
	} else {
		ec, err := executionclient.NewMulti(
			ctx,
			executionAddrList,
			ssvNetworkConfig.RegistryContractAddr,
			executionclient.WithLoggerMulti(logger),
			executionclient.WithReqTimeoutMulti(cfg.ExecutionClient.ConnectionTimeout),
			executionclient.WithSyncDistanceToleranceMulti(cfg.ExecutionClient.SyncDistanceTolerance),
		)
		if err != nil {
			return fmt.Errorf("could not connect to execution client: %w", err)
		}

		executionClient = ec
	}

	a, err := assemble(ctx, cfg, logger, res, networkConfig, consensusClient, executionClient)
	if err != nil {
		return err
	}
	defer func() {
		if err := a.Close(); err != nil {
			logger.Error("could not cleanly close node", zap.Error(err))
		}
	}()

	return a.start(ctx)
}
