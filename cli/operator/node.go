package operator

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/api/handlers"
	apiserver "github.com/bloxapp/ssv/api/server"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	exporterapi "github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/api/decided"
	ssv_identity "github.com/bloxapp/ssv/identity"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/migrations"
	"github.com/bloxapp/ssv/monitoring/metrics"
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
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/format"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options         `yaml:"db"`
	SSVOptions                 operator.Options       `yaml:"ssv"`
	ETH1Options                eth1.Options           `yaml:"eth1"`
	ETH2Options                beaconprotocol.Options `yaml:"eth2"`
	P2pNetworkConfig           p2pv1.Config           `yaml:"p2p"`

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

// StartNodeCmd is the command to start SSV node
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
		db, err := setupDb(logger, networkConfig.Beacon)
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

		ctx := cmd.Context()
		slotTicker := slot_ticker.NewTicker(ctx, networkConfig)

		cfg.ETH2Options.Context = cmd.Context()
		eth2Client, eth1Client := setupNodes(logger, operatorData.ID, networkConfig, slotTicker)

		nodeChecker := nodeprobe.NewProber(
			logger,
			eth1Client,
			// Underlying options.Beacon's value implements nodechecker.StatusChecker.
			// However, as it uses spec's specssv.BeaconNode interface, avoiding type assertion requires modifications in spec.
			// If options.Beacon doesn't implement nodechecker.StatusChecker due to a mistake, this would panic early.
			eth2Client.(nodeprobe.StatusChecker),
		)

		nodeChecker.Start(ctx)

		cfg.SSVOptions.ForkVersion = forkVersion
		cfg.SSVOptions.Context = ctx
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.BeaconNode = eth2Client
		cfg.SSVOptions.Eth1Client = eth1Client
		cfg.SSVOptions.Network = networkConfig
		cfg.SSVOptions.P2PNetwork = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.ForkVersion = forkVersion
		cfg.SSVOptions.ValidatorOptions.BeaconNetwork = networkConfig.Beacon
		cfg.SSVOptions.ValidatorOptions.Context = ctx
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.KeyManager = keyManager
		cfg.SSVOptions.ValidatorOptions.Beacon = eth2Client

		cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider = nodeStorage.GetPrivateKey
		cfg.SSVOptions.ValidatorOptions.OperatorData = operatorData
		cfg.SSVOptions.ValidatorOptions.RegistryStorage = nodeStorage
		cfg.SSVOptions.ValidatorOptions.GasLimit = cfg.ETH2Options.GasLimit

		if cfg.WsAPIPort != 0 {
			ws := exporterapi.NewWsServer(cmd.Context(), nil, http.NewServeMux(), cfg.WithPing)
			cfg.SSVOptions.WS = ws
			cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
			cfg.SSVOptions.ValidatorOptions.NewDecidedHandler = decided.NewStreamPublisher(logger, ws)
		}

		cfg.SSVOptions.ValidatorOptions.DutyRoles = []spectypes.BeaconRole{spectypes.BNRoleAttester} // TODO could be better to set in other place
		validatorCtrl := validator.NewController(logger, cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl

		operatorNode = operator.New(logger, cfg.SSVOptions, slotTicker)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(cmd.Context(), logger, db, cfg.MetricsAPIPort, cfg.EnableProfile)
		}

		nodeChecker.Wait()

		nodeUnreadyHandler := func() {
			logger.Fatal("Ethereum node(s) are either out of sync or down. Ensure the nodes are ready to resume.")
		}
		nodeChecker.SetUnreadyHandler(nodeUnreadyHandler)

		metrics.ReportSSVNodeHealthiness(true)

		// load & parse local events yaml if exists, otherwise sync from contract
		if len(cfg.LocalEventsPath) > 0 {
			if err := validator.LoadLocalEvents(
				logger,
				validatorCtrl.Eth1EventHandler(logger, false),
				cfg.LocalEventsPath,
			); err != nil {
				logger.Fatal("failed to load local events", zap.Error(err))
			}
		} else {
			if err := operatorNode.StartEth1(logger, networkConfig.ETH1SyncOffset); err != nil {
				logger.Fatal("failed to start eth1", zap.Error(err))
			}
		}

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

func setupDb(logger *zap.Logger, eth2Network beaconprotocol.Network) (basedb.IDb, error) {
	db, err := storage.GetStorageFactory(logger, cfg.DBOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open db")
	}
	reopenDb := func() error {
		if err := db.Close(logger); err != nil {
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
	if _, ok := db.(basedb.GarbageCollector); ok {
		// Close & reopen the database to trigger any unknown internal
		// startup/shutdown procedures that the storage engine may have.
		start := time.Now()
		if err := reopenDb(); err != nil {
			return nil, err
		}

		// Run a long garbage collection cycle with a timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		if err := db.(basedb.GarbageCollector).FullGC(ctx); err != nil {
			return nil, errors.Wrap(err, "failed to collect garbage")
		}

		// Close & reopen again.
		if err := reopenDb(); err != nil {
			return nil, err
		}
		logger.Info("post-migrations garbage collection completed", fields.Duration(start))
	}

	return db, nil
}

func setupOperatorStorage(logger *zap.Logger, db basedb.IDb) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}
	operatorPubKey, err := nodeStorage.SetupPrivateKey(logger, cfg.OperatorPrivateKey, cfg.GenerateOperatorPrivateKey)
	if err != nil {
		logger.Fatal("could not setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(logger, operatorPubKey)
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
		fields.Network(cfg.SSVOptions.NetworkName),
		fields.Domain(networkConfig.Domain),
		fields.Fork(forkVersion),
		fields.Config(networkConfig),
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

func setupNodes(
	logger *zap.Logger,
	operatorID spectypes.OperatorID,
	network networkconfig.NetworkConfig,
	slotTicker slot_ticker.Ticker,
) (beaconprotocol.BeaconNode, eth1.Client) {
	// consensus client
	cfg.ETH2Options.Graffiti = []byte("SSV.Network")
	cfg.ETH2Options.GasLimit = spectypes.DefaultGasLimit
	cfg.ETH2Options.Network = network.Beacon
	eth2Client, err := goclient.New(logger, cfg.ETH2Options, operatorID, slotTicker)
	if err != nil {
		logger.Fatal("failed to create beacon go-client", zap.Error(err),
			fields.Address(cfg.ETH2Options.BeaconNodeAddr))
	}

	// execution client
	logger.Info("using registry contract address", fields.Address(network.RegistryContractAddr), fields.ABIVersion(cfg.ETH1Options.AbiVersion.String()))
	if len(cfg.ETH1Options.RegistryContractABI) > 0 {
		logger.Info("using registry contract abi", fields.ABI(cfg.ETH1Options.RegistryContractABI))
		if err = eth1.LoadABI(logger, cfg.ETH1Options.RegistryContractABI); err != nil {
			logger.Fatal("failed to load ABI JSON", zap.Error(err))
		}
	}
	eth1Client, err := goeth.NewEth1Client(logger, goeth.ClientOptions{
		Ctx:                  cfg.ETH2Options.Context,
		NodeAddr:             cfg.ETH1Options.ETH1Addr,
		ConnectionTimeout:    cfg.ETH1Options.ETH1ConnectionTimeout,
		ContractABI:          eth1.ContractABI(cfg.ETH1Options.AbiVersion),
		RegistryContractAddr: network.RegistryContractAddr,
		AbiVersion:           cfg.ETH1Options.AbiVersion,
	})
	if err != nil {
		logger.Fatal("failed to create eth1 client", zap.Error(err))
	}

	return eth2Client, eth1Client
}

func startMetricsHandler(ctx context.Context, logger *zap.Logger, db basedb.IDb, port int, enableProf bool) {
	logger = logger.Named(logging.NameMetricsHandler)
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(ctx, db, enableProf, operatorNode.(metrics.HealthCheckAgent))
	addr := fmt.Sprintf(":%d", port)
	if err := metricsHandler.Start(logger, http.NewServeMux(), addr); err != nil {
		logger.Panic("failed to serve metrics", zap.Error(err))
	}
}
