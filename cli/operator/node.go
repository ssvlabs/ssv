package operator

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/api/handlers"
	apiserver "github.com/bloxapp/ssv/api/server"
	"github.com/bloxapp/ssv/beacon/goclient"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/eth/eventdatahandler"
	"github.com/bloxapp/ssv/eth/eventdispatcher"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/localevents"
	exporterapi "github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/api/decided"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	ssv_identity "github.com/bloxapp/ssv/identity"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/migrations"
	"github.com/bloxapp/ssv/monitoring/metrics"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/network"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/nodeprobe"
	"github.com/bloxapp/ssv/operator"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/format"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options                   `yaml:"db"`
	SSVOptions                 operator.Options                 `yaml:"ssv"`
	ExecutionClient            executionclient.ExecutionOptions `yaml:"eth1"` // TODO: execution_client in yaml
	ConsensusClient            beaconprotocol.Options           `yaml:"eth2"` // TODO: consensus_client in yaml
	P2pNetworkConfig           p2pv1.Config                     `yaml:"p2p"`

	OperatorPrivateKey         string `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
	GenerateOperatorPrivateKey bool   `yaml:"GenerateOperatorPrivateKey" env:"GENERATE_OPERATOR_KEY" env-description:"Whether to generate operator key if none is passed by config"`
	MetricsAPIPort             int    `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"Port to listen on for the metrics API."`
	EnableProfile              bool   `yaml:"EnableProfile" env:"ENABLE_PROFILE" env-description:"flag that indicates whether go profiling tools are enabled"`
	NetworkPrivateKey          string `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"private key for network identity"`

	WsAPIPort int  `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-description:"Port to listen on for the websocket API."`
	WithPing  bool `yaml:"WithPing" env:"WITH_PING" env-description:"Whether to send websocket ping messages'"`

	SSVAPIPort int `yaml:"SSVAPIPort" env:"SSV_API_PORT" env-description:"Port to listen on for the SSV API."`

	LocalEventsPath string `yaml:"LocalEventsPath" env:"EVENTS_PATH" env-description:"path to local events"`
}

var cfg config

var globalArgs global_config.Args

var operatorNode operator.Node

var StartNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		logger, err := setupGlobal(cmd)
		if err != nil {
			log.Fatal("could not create logger", err)
		}

		defer logging.CapturePanic(logger)

		networkConfig, forkVersion, err := setupSSVNetwork(logger)
		if err != nil {
			log.Fatal("could not setup network", err)
		}

		cfg.DBOptions.Ctx = cmd.Context()
		db, err := setupDB(logger, networkConfig.Beacon)
		if err != nil {
			logger.Fatal("could not setup db", zap.Error(err))
		}
		nodeStorage, operatorData := setupOperatorStorage(logger, db)

		keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, networkConfig, cfg.SSVOptions.ValidatorOptions.BuilderProposals)
		if err != nil {
			logger.Fatal("could not create new eth-key-manager signer", zap.Error(err))
		}

		cfg.P2pNetworkConfig.Ctx = cmd.Context()

		permissioned := func() bool {
			currentEpoch := uint64(networkConfig.Beacon.EstimatedCurrentEpoch())
			return currentEpoch >= cfg.P2pNetworkConfig.PermissionedActivateEpoch && currentEpoch < cfg.P2pNetworkConfig.PermissionedDeactivateEpoch
		}

		cfg.P2pNetworkConfig.Permissioned = permissioned
		cfg.P2pNetworkConfig.WhitelistedOperatorKeys = append(cfg.P2pNetworkConfig.WhitelistedOperatorKeys, networkConfig.WhitelistedOperatorKeys...)

		p2pNetwork := setupP2P(forkVersion, operatorData, db, logger, networkConfig)

		slotTicker := slot_ticker.NewTicker(cmd.Context(), networkConfig)

		metricsReporter := metricsreporter.New(
			metricsreporter.WithLogger(logger),
		)

		cfg.ConsensusClient.Context = cmd.Context()

		cfg.ConsensusClient.Graffiti = []byte("SSV.Network")
		cfg.ConsensusClient.GasLimit = spectypes.DefaultGasLimit
		cfg.ConsensusClient.Network = networkConfig.Beacon

		consensusClient := setupConsensusClient(logger, operatorData.ID, slotTicker)

		executionClient := executionclient.New(
			cfg.ExecutionClient.Addr,
			ethcommon.HexToAddress(networkConfig.RegistryContractAddr),
			executionclient.WithLogger(logger),
			executionclient.WithMetrics(metricsReporter),
			executionclient.WithFinalizationOffset(executionclient.DefaultFinalizationOffset),
			executionclient.WithConnectionTimeout(cfg.ExecutionClient.ConnectionTimeout),
			executionclient.WithReconnectionInitialInterval(executionclient.DefaultReconnectionInitialInterval),
			executionclient.WithReconnectionMaxInterval(executionclient.DefaultReconnectionMaxInterval),
		)

		executionClient.Connect(cmd.Context())

		nodeProber := nodeprobe.NewProber(
			logger,
			executionClient,
			// Underlying options.Beacon's value implements nodeprobe.StatusChecker.
			// However, as it uses spec's specssv.BeaconNode interface, avoiding type assertion requires modifications in spec.
			// If options.Beacon doesn't implement nodeprobe.StatusChecker due to a mistake, this would panic early.
			consensusClient.(nodeprobe.StatusChecker),
		)

		nodeProber.Start(cmd.Context())

		cfg.SSVOptions.ForkVersion = forkVersion
		cfg.SSVOptions.Context = cmd.Context()
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.BeaconNode = consensusClient
		cfg.SSVOptions.ExecutionClient = executionClient
		cfg.SSVOptions.Network = networkConfig
		cfg.SSVOptions.P2PNetwork = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.ForkVersion = forkVersion
		cfg.SSVOptions.ValidatorOptions.BeaconNetwork = networkConfig.Beacon
		cfg.SSVOptions.ValidatorOptions.Context = cmd.Context()
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.Beacon = consensusClient
		cfg.SSVOptions.ValidatorOptions.KeyManager = keyManager

		cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider = nodeStorage.GetPrivateKey
		cfg.SSVOptions.ValidatorOptions.OperatorData = operatorData
		cfg.SSVOptions.ValidatorOptions.RegistryStorage = nodeStorage
		cfg.SSVOptions.ValidatorOptions.GasLimit = cfg.ConsensusClient.GasLimit

		if cfg.WsAPIPort != 0 {
			ws := exporterapi.NewWsServer(cmd.Context(), nil, http.NewServeMux(), cfg.WithPing)
			cfg.SSVOptions.WS = ws
			cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
			cfg.SSVOptions.ValidatorOptions.NewDecidedHandler = decided.NewStreamPublisher(logger, ws)
		}

		cfg.SSVOptions.ValidatorOptions.DutyRoles = []spectypes.BeaconRole{spectypes.BNRoleAttester} // TODO could be better to set in other place

		storageRoles := []spectypes.BeaconRole{
			spectypes.BNRoleAttester,
			spectypes.BNRoleProposer,
			spectypes.BNRoleAggregator,
			spectypes.BNRoleSyncCommittee,
			spectypes.BNRoleSyncCommitteeContribution,
			spectypes.BNRoleValidatorRegistration,
		}
		storageMap := ibftstorage.NewStores()

		for _, storageRole := range storageRoles {
			storageMap.Add(storageRole, ibftstorage.New(cfg.SSVOptions.ValidatorOptions.DB, storageRole.String(), cfg.SSVOptions.ValidatorOptions.ForkVersion))
		}

		cfg.SSVOptions.ValidatorOptions.StorageMap = storageMap
		cfg.SSVOptions.ValidatorOptions.Metrics = metricsReporter

		validatorCtrl := validator.NewController(logger, cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl
		cfg.SSVOptions.Metrics = metricsReporter

		operatorNode = operator.New(logger, cfg.SSVOptions, slotTicker)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(cmd.Context(), logger, db, metricsReporter, cfg.MetricsAPIPort, cfg.EnableProfile)
		}

		nodeProber.Wait()

		nodeProber.SetUnreadyHandler(func() {
			logger.Fatal("Ethereum node(s) are either out of sync or down. Ensure the nodes are ready to resume.")
		})

		metricsReporter.SSVNodeHealthy()

		setupEventHandling(
			cmd.Context(),
			logger,
			executionClient,
			validatorCtrl,
			storageMap,
			metricsReporter,
			nodeProber,
			networkConfig,
			nodeStorage,
		)

		cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
			return validatorCtrl.GetValidatorStats()
		}
		if err := p2pNetwork.Setup(logger); err != nil {
			logger.Fatal("failed to setup network", zap.Error(err))
		}
		if err := p2pNetwork.Start(logger); err != nil {
			logger.Fatal("failed to start network", zap.Error(err))
		}

		if cfg.SSVAPIPort > 0 {
			apiServer := apiserver.New(
				logger,
				fmt.Sprintf(":%d", cfg.SSVAPIPort),
				&handlers.Node{
					// TODO: replace with narrower interface! (instead of accessing the entire PeersIndex)
					PeersIndex: p2pNetwork.(p2pv1.PeersIndexProvider).PeersIndex(),
					Network:    p2pNetwork.(p2pv1.HostProvider).Host().Network(),
					TopicIndex: p2pNetwork.(handlers.TopicIndex),
				},
				&handlers.Validators{
					Shares: nodeStorage.Shares(),
				},
			)
			go func() {
				err := apiServer.Run()
				if err != nil {
					logger.Fatal("failed to start API server", zap.Error(err))
				}
			}()
		}

		if err := operatorNode.Start(logger); err != nil {
			logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}

func setupGlobal(cmd *cobra.Command) (*zap.Logger, error) {
	commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)
	log.Printf("starting SSV node (version %s)", commons.GetBuildData())

	if globalArgs.ConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			return nil, fmt.Errorf("could not read config: %w", err)
		}
	}
	if globalArgs.ShareConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
			return nil, fmt.Errorf("could not read share config: %w", err)
		}
	}

	if err := logging.SetGlobalLogger(cfg.LogLevel, cfg.LogLevelFormat, cfg.LogFormat, cfg.LogFilePath); err != nil {
		return nil, fmt.Errorf("logging.SetGlobalLogger: %w", err)
	}

	return zap.L(), nil
}

func setupDB(logger *zap.Logger, eth2Network beaconprotocol.Network) (*kv.BadgerDB, error) {
	db, err := storage.GetStorageFactory(logger, cfg.DBOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open db")
	}
	reopenDb := func() error {
		if err := db.Close(); err != nil {
			return errors.Wrap(err, "failed to close db")
		}
		db, err = storage.GetStorageFactory(logger, cfg.DBOptions)
		return errors.Wrap(err, "failed to reopen db")
	}

	migrationOpts := migrations.Options{
		Db:      db,
		DbPath:  cfg.DBOptions.Path,
		Network: eth2Network,
	}
	applied, err := migrations.Run(cfg.DBOptions.Ctx, logger, migrationOpts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to run migrations")
	}
	if applied == 0 {
		return db, nil
	}

	// If migrations were applied, we run a full garbage collection cycle
	// to reclaim any space that may have been freed up.
	// Close & reopen the database to trigger any unknown internal
	// startup/shutdown procedures that the storage engine may have.
	start := time.Now()
	if err := reopenDb(); err != nil {
		return nil, err
	}

	// Run a long garbage collection cycle with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := db.FullGC(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to collect garbage")
	}

	// Close & reopen again.
	if err := reopenDb(); err != nil {
		return nil, err
	}
	logger.Info("post-migrations garbage collection completed", fields.Duration(start))

	return db, nil
}

func setupOperatorStorage(logger *zap.Logger, db basedb.IDb) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}
	operatorPubKey, err := nodeStorage.SetupPrivateKey(cfg.OperatorPrivateKey, cfg.GenerateOperatorPrivateKey)
	if err != nil {
		logger.Fatal("could not setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(nil, operatorPubKey)
	if err != nil {
		logger.Fatal("could not get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey: operatorPubKey,
		}
	}

	return nodeStorage, operatorData
}

func setupSSVNetwork(logger *zap.Logger) (networkconfig.NetworkConfig, forksprotocol.ForkVersion, error) {
	networkConfig, err := networkconfig.GetNetworkConfigByName(cfg.SSVOptions.NetworkName)
	if err != nil {
		return networkconfig.NetworkConfig{}, "", err
	}

	types.SetDefaultDomain(networkConfig.Domain)

	currentEpoch := networkConfig.Beacon.EstimatedCurrentEpoch()
	forkVersion := forksprotocol.GetCurrentForkVersion(currentEpoch)

	logger.Info("setting ssv network",
		fields.Network(networkConfig.Name),
		fields.Domain(networkConfig.Domain),
		fields.Fork(forkVersion),
		zap.Any("beaconNetwork", networkConfig.Beacon.BeaconNetwork),
		zap.Uint64("genesisEpoch", uint64(networkConfig.GenesisEpoch)),
		zap.String("registryContract", networkConfig.RegistryContractAddr),
		zap.Int64("registrySyncOffset", networkConfig.RegistrySyncOffset.Int64()),
	)

	return networkConfig, forkVersion, nil
}

func setupP2P(
	forkVersion forksprotocol.ForkVersion,
	operatorData *registrystorage.OperatorData,
	db basedb.IDb,
	logger *zap.Logger,
	network networkconfig.NetworkConfig,
) network.P2PNetwork {
	istore := ssv_identity.NewIdentityStore(db)
	netPrivKey, err := istore.SetupNetworkKey(logger, cfg.NetworkPrivateKey)
	if err != nil {
		logger.Fatal("failed to setup network private key", zap.Error(err))
	}

	cfg.P2pNetworkConfig.NodeStorage, err = operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	cfg.P2pNetworkConfig.NetworkPrivateKey = netPrivKey
	cfg.P2pNetworkConfig.ForkVersion = forkVersion
	cfg.P2pNetworkConfig.OperatorID = format.OperatorID(operatorData.PublicKey)
	cfg.P2pNetworkConfig.FullNode = cfg.SSVOptions.ValidatorOptions.FullNode
	cfg.P2pNetworkConfig.Network = network

	return p2pv1.New(logger, &cfg.P2pNetworkConfig)
}

func setupConsensusClient(
	logger *zap.Logger,
	operatorID spectypes.OperatorID,
	slotTicker slot_ticker.Ticker,
) beaconprotocol.BeaconNode {
	cl, err := goclient.New(logger, cfg.ConsensusClient, operatorID, slotTicker)
	if err != nil {
		logger.Fatal("failed to create beacon go-client", zap.Error(err),
			fields.Address(cfg.ConsensusClient.BeaconNodeAddr))
	}

	return cl
}

func setupEventHandling(
	ctx context.Context,
	logger *zap.Logger,
	executionClient *executionclient.ExecutionClient,
	validatorCtrl validator.Controller,
	storageMap *ibftstorage.QBFTStores,
	metricsReporter *metricsreporter.MetricsReporter,
	nodeProber *nodeprobe.Prober,
	networkConfig networkconfig.NetworkConfig,
	nodeStorage operatorstorage.Storage,
) {
	eventFilterer, err := executionClient.Filterer()
	if err != nil {
		logger.Fatal("failed to set up event filterer", zap.Error(err))
	}

	contractABI, err := contract.ContractMetaData.GetAbi()
	if err != nil {
		logger.Fatal("failed to get contract ABI", zap.Error(err))
	}

	eventDataHandler, err := eventdatahandler.New(
		nodeStorage,
		eventFilterer,
		contractABI,
		validatorCtrl,
		networkConfig.Domain,
		cfg.SSVOptions.ValidatorOptions.OperatorData,
		cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider,
		cfg.SSVOptions.ValidatorOptions.KeyManager,
		cfg.SSVOptions.ValidatorOptions.Beacon,
		storageMap,
		eventdatahandler.WithFullNode(),
		eventdatahandler.WithLogger(logger),
		eventdatahandler.WithMetrics(metricsReporter),
	)
	if err != nil {
		logger.Fatal("failed to setup event data handler", zap.Error(err))
	}

	eventBatcher := eventbatcher.NewEventBatcher()
	eventDispatcher := eventdispatcher.New(
		executionClient,
		eventBatcher,
		eventDataHandler,
		eventdispatcher.WithLogger(logger),
		eventdispatcher.WithMetrics(metricsReporter),
		eventdispatcher.WithNodeProber(nodeProber),
	)

	fromBlock, found, err := nodeStorage.GetLastProcessedBlock(nil)
	if err != nil {
		logger.Fatal("could not get last processed block", zap.Error(err))
	}

	if !found || fromBlock == nil {
		fromBlock = networkConfig.RegistrySyncOffset
		logger.Info("no last processed block in DB found, using last processed block from network config",
			fields.BlockNumber(fromBlock.Uint64()))
	} else {
		logger.Info("using last processed block from DB",
			fields.BlockNumber(fromBlock.Uint64()))
	}

	// load & parse local events yaml if exists, otherwise sync from contract
	if len(cfg.LocalEventsPath) != 0 {
		localEvents, err := localevents.Load(cfg.LocalEventsPath)
		if err != nil {
			logger.Fatal("failed to load local events", zap.Error(err))
		}

		if err := eventDataHandler.HandleLocalEvents(localEvents); err != nil {
			logger.Fatal("error occurred while running event dispatcher", zap.Error(err))
		}
	} else {
		if err := eventDispatcher.Start(ctx, fromBlock.Uint64()); err != nil {
			logger.Fatal("error occurred while running event dispatcher", zap.Error(err))
		}
	}
}

func startMetricsHandler(ctx context.Context, logger *zap.Logger, db basedb.IDb, metricsReporter *metricsreporter.MetricsReporter, port int, enableProf bool) {
	logger = logger.Named(logging.NameMetricsHandler)
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(ctx, db, metricsReporter, enableProf, operatorNode.(metrics.HealthCheckAgent))
	addr := fmt.Sprintf(":%d", port)
	if err := metricsHandler.Start(logger, http.NewServeMux(), addr); err != nil {
		logger.Panic("failed to serve metrics", zap.Error(err))
	}
}
