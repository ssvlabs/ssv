package operator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	hexporter "github.com/ssvlabs/ssv/api/handlers/exporter"
	hnode "github.com/ssvlabs/ssv/api/handlers/node"
	hvalidators "github.com/ssvlabs/ssv/api/handlers/validators"
	apiserver "github.com/ssvlabs/ssv/api/server"
	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/hprobe"
	networkcommons "github.com/ssvlabs/ssv/network/commons"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/metrics"
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

	if a.cfg.MetricsAPIPort > 0 {
		go func() {
			metricsHandler := metrics.NewHandler(a.logger, a.db, a.cfg.EnableProfile, a.operatorNode)
			if err := metricsHandler.Start(http.NewServeMux(), fmt.Sprintf(":%d", a.cfg.MetricsAPIPort)); err != nil {
				a.logger.Fatal("failed to serve metrics", zap.Error(err))
			}
		}()
	}

	healthProber := hprobe.NewHealthProber(a.logger)
	healthProber.AddComponent(clComponentName, a.consensusClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	healthProber.AddComponent(elComponentName, a.executionClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	if err := ensureComponentsHealthy(ctx, a.logger, healthProber); err != nil {
		return err
	}

	eventSyncer, err := syncContractEvents(
		ctx,
		a.logger,
		a.cfg,
		a.executionClient,
		a.validatorCtrl,
		a.networkConfig,
		a.nodeStorage,
		a.operatorDataStore,
		a.keyManager,
		a.doppelgangerHandler,
	)
	if err != nil {
		return err
	}
	if len(a.cfg.LocalEventsPath) == 0 {
		healthProber.AddComponent(eventSyncerComponentName, eventSyncer, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	}
	go startHealthProber(ctx, a.logger, healthProber)

	if _, err := a.metadataSyncer.SyncAll(ctx); err != nil {
		return fmt.Errorf("failed to sync metadata on startup: %w", err)
	}

	if a.usingSSVSigner {
		if err := ensureNoMissingKeys(ctx, a.nodeStorage, a.operatorDataStore, a.ssvSignerClient); err != nil {
			return err
		}
	}

	// Increase MaxPeers if the operator is subscribed to many subnets.
	// TODO: use OperatorCommittees when it's fixed.
	if a.cfg.P2pNetworkConfig.DynamicMaxPeers {
		var (
			baseMaxPeers        = 60
			maxPeersLimit       = a.cfg.P2pNetworkConfig.DynamicMaxPeersLimit
			idealPeersPerSubnet = 3
		)
		start := time.Now()
		myValidators := a.nodeStorage.ValidatorStore().OperatorValidators(a.operatorDataStore.GetOperatorID())
		mySubnets := networkcommons.Subnets{}
		myActiveSubnets := 0
		for _, v := range myValidators {
			subnet := networkcommons.CommitteeSubnet(v.CommitteeID())
			if !mySubnets.IsSet(subnet) {
				mySubnets.Set(subnet)
				myActiveSubnets++
			}
		}
		idealMaxPeers := min(baseMaxPeers+idealPeersPerSubnet*myActiveSubnets, maxPeersLimit)
		if a.cfg.P2pNetworkConfig.MaxPeers < idealMaxPeers {
			a.logger.Warn("increasing MaxPeers to match the operator's subscribed subnets",
				zap.Int("old_max_peers", a.cfg.P2pNetworkConfig.MaxPeers),
				zap.Int("new_max_peers", idealMaxPeers),
				zap.Int("subscribed_subnets", myActiveSubnets),
				fields.Took(time.Since(start)),
			)
			a.cfg.P2pNetworkConfig.MaxPeers = idealMaxPeers
		}
	}

	// Wire validator stats into pubsub peer scoring, then set up and start the p2p network.
	// These run unconditionally: gating them on DynamicMaxPeers (a long-standing bug) left
	// DynamicMaxPeers=false nodes with a network that was never Setup/Start'd — operator.Node.Start
	// then failed Subscribe* with ErrNetworkIsNotReady — and with a nil GetValidatorStats, which
	// makes pubsub peer scoring silently fall back to fake hard-coded stats. GetValidatorStats
	// must be set before Setup(), which reads it via setupPubsub.
	a.cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
		return a.validatorCtrl.GetValidatorStats()
	}
	if err := a.p2pNetwork.Setup(); err != nil {
		return fmt.Errorf("failed to setup network: %w", err)
	}
	if err := a.p2pNetwork.Start(); err != nil {
		return fmt.Errorf("failed to start network: %w", err)
	}
	healthProber.AddComponent(p2pComponentName, a.p2pNetwork.(p2pv1.HealthChecker), proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)

	if a.cfg.SSVAPIPort > 0 {
		warnIfSSVAPIAddressUnset(a.logger, a.cfg.SSVAPIAddress, a.cfg.SSVAPIPort)
		apiServer := apiserver.New(
			a.logger,
			net.JoinHostPort(a.cfg.SSVAPIAddress, strconv.Itoa(a.cfg.SSVAPIPort)),
			hnode.NewNode(
				// TODO: replace with narrower interface! (instead of accessing the entire PeersIndex)
				[]string{fmt.Sprintf("tcp://%s:%d", a.cfg.P2pNetworkConfig.HostAddress, a.cfg.P2pNetworkConfig.TCPPort), fmt.Sprintf("udp://%s:%d", a.cfg.P2pNetworkConfig.HostAddress, a.cfg.P2pNetworkConfig.UDPPort)},
				a.p2pNetwork.(p2pv1.PeersIndexProvider).PeersIndex(),
				a.p2pNetwork.(p2pv1.HostProvider).Host().Network(),
				a.p2pNetwork,
				healthProber,
				clComponentName,
				elComponentName,
				eventSyncerComponentName,
			),
			&hvalidators.Validators{
				Shares: a.nodeStorage.Shares(),
			},
			hexporter.NewExporter(a.logger, a.storageMap, a.collector, a.nodeStorage.ValidatorStore()),
			a.mode == modeExporterArchive,
		)
		go func() {
			err := apiServer.Run()
			if err != nil {
				a.logger.Fatal("failed to start API server", zap.Error(err))
			}
		}()
	}
	if err := a.operatorNode.Start(a.cfg.SSVOptions.Context); err != nil {
		return fmt.Errorf("failed to start SSV node: %w", err)
	}

	return nil
}
