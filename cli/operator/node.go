package operator

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
	logging "github.com/ipfs/go-log"
	"log"
	"net/http"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/prysmaticlabs/prysm/time/slots"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/exporter/api"
	ssv_identity "github.com/bloxapp/ssv/identity"
	"github.com/bloxapp/ssv/migrations"
	"github.com/bloxapp/ssv/monitoring/metrics"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/operator"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/utils/rsaencryption"
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
	MetricsAPIPort             int    `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"port of metrics api"`
	EnableProfile              bool   `yaml:"EnableProfile" env:"ENABLE_PROFILE" env-description:"flag that indicates whether go profiling tools are enabled"`
	NetworkPrivateKey          string `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"private key for network identity"`

	WsAPIPort int  `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-description:"port of WS API"`
	WithPing  bool `yaml:"WithPing" env:"WITH_PING" env-description:"Whether to send websocket ping messages'"`

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
		logger := setupGlobal(cmd)

		eth2Network, forkVersion := setupSSVNetwork(logger)

		cfg.DBOptions.Ctx = cmd.Context()
		db := setupDb(logger, eth2Network)
		operatorStorage, operatorPubKey := setupOperatorStorage(db)

		keyManager, err := ekm.NewETHKeyManagerSigner(db, eth2Network, types.GetDefaultDomain(), logger)
		if err != nil {
			logger.Fatal("could not create new eth-key-manager signer", zap.Error(err))
		}

		cfg.P2pNetworkConfig.Ctx = cmd.Context()
		p2pNetwork := setupP2P(forkVersion, operatorPubKey, db, logger)

		cfg.ETH2Options.Context = cmd.Context()
		el, cl := setupNodes(logger)

		ctx := cmd.Context()
		cfg.SSVOptions.ForkVersion = forkVersion
		cfg.SSVOptions.Context = ctx
		cfg.SSVOptions.Logger = logger
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.Beacon = el
		cfg.SSVOptions.ETHNetwork = eth2Network
		cfg.SSVOptions.Network = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.ForkVersion = forkVersion
		cfg.SSVOptions.ValidatorOptions.ETHNetwork = eth2Network
		cfg.SSVOptions.ValidatorOptions.Logger = logger
		cfg.SSVOptions.ValidatorOptions.Context = ctx
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.Beacon = el
		cfg.SSVOptions.ValidatorOptions.KeyManager = keyManager
		cfg.SSVOptions.ValidatorOptions.CleanRegistryData = cfg.ETH1Options.CleanRegistryData

		cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider = operatorStorage.GetPrivateKey
		cfg.SSVOptions.ValidatorOptions.OperatorPubKey = operatorPubKey
		cfg.SSVOptions.ValidatorOptions.RegistryStorage = operatorStorage

		cfg.SSVOptions.Eth1Client = cl

		if cfg.WsAPIPort != 0 {
			ws := api.NewWsServer(cmd.Context(), logger, nil, http.NewServeMux(), cfg.WithPing)
			cfg.SSVOptions.WS = ws
			cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
			//cfg.SSVOptions.ValidatorOptions.NewDecidedHandler = decided.NewStreamPublisher(Logger, ws)
		}

		cfg.SSVOptions.ValidatorOptions.DutyRoles = []spectypes.BeaconRole{spectypes.BNRoleAttester} // TODO could be better to set in other place
		validatorCtrl := validator.NewController(cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl

		operatorNode = operator.New(cfg.SSVOptions)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(cmd.Context(), logger, cfg.MetricsAPIPort, cfg.EnableProfile)
		}

		metrics.WaitUntilHealthy(logger, cfg.SSVOptions.Eth1Client, "execution client")
		metrics.WaitUntilHealthy(logger, cfg.SSVOptions.Beacon, "consensus client")

		// load & parse local events yaml if exists, otherwise sync from contract
		if len(cfg.LocalEventsPath) > 0 {
			if err := validator.LoadLocalEvents(
				logger,
				validatorCtrl.Eth1EventHandler(false),
				cfg.LocalEventsPath,
			); err != nil {
				logger.Fatal("failed to load local events", zap.Error(err))
			}
		} else {
			if err := operatorNode.StartEth1(eth1.HexStringToSyncOffset(cfg.ETH1Options.ETH1SyncOffset)); err != nil {
				logger.Fatal("failed to start eth1", zap.Error(err))
			}
		}

		cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
			return validatorCtrl.GetValidatorStats()
		}
		if err := p2pNetwork.Setup(); err != nil {
			logger.Fatal("failed to setup network", zap.Error(err))
		}
		if err := p2pNetwork.Start(); err != nil {
			logger.Fatal("failed to start network", zap.Error(err))
		}
		if err := operatorNode.Start(); err != nil {
			logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}

func setupGlobal(cmd *cobra.Command) *zap.Logger {
	commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)
	log.Printf("starting %s", commons.GetBuildData())
	if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
		log.Fatalf("could not read config %s", err)
	}
	if globalArgs.ShareConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
			log.Fatalf("could not read share config %s", err)
		}
	}
	loggerLevel, errLogLevel := logex.GetLoggerLevelValue(cfg.LogLevel)
	logger := logex.Build(commons.GetBuildData(), loggerLevel, &logex.EncodingConfig{
		Format:       cfg.GlobalConfig.LogFormat,
		LevelEncoder: logex.LevelEncoder([]byte(cfg.LogLevelFormat)),
	})
	if errLogLevel != nil {
		logger.Warn(fmt.Sprintf("Default log level set to %s", loggerLevel), zap.Error(errLogLevel))
	}
	if len(cfg.DebugServices) > 0 {
		_ = logging.SetLogLevelRegex(cfg.DebugServices, "debug")
	}

	return logger
}

func setupDb(logger *zap.Logger, eth2Network beaconprotocol.Network) basedb.IDb {
	cfg.DBOptions.Logger = logger

	db, err := storage.GetStorageFactory(cfg.DBOptions)
	if err != nil {
		logger.Fatal("failed to create db!", zap.Error(err))
	}

	migrationOpts := migrations.Options{
		Db:      db,
		Logger:  logger,
		DbPath:  cfg.DBOptions.Path,
		Network: eth2Network,
	}
	err = migrations.Run(cfg.DBOptions.Ctx, migrationOpts)
	if err != nil {
		logger.Fatal("failed to run migrations", zap.Error(err))
	}

	return db
}

func setupOperatorStorage(db basedb.IDb) (operatorstorage.Storage, string) {
	nodeStorage := operatorstorage.NewNodeStorage(db, cfg.DBOptions.Logger)
	if err := nodeStorage.SetupPrivateKey(cfg.GenerateOperatorPrivateKey, cfg.OperatorPrivateKey); err != nil {
		cfg.DBOptions.Logger.Fatal("failed to setup operator private key", zap.Error(err))
	}
	operatorPrivateKey, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		cfg.DBOptions.Logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	operatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivateKey)
	if err != nil {
		cfg.DBOptions.Logger.Fatal("failed to extract operator public key", zap.Error(err))
	}
	return nodeStorage, operatorPubKey
}

func setupSSVNetwork(logger *zap.Logger) (beaconprotocol.Network, forksprotocol.ForkVersion) {
	if len(cfg.P2pNetworkConfig.NetworkID) == 0 {
		cfg.P2pNetworkConfig.NetworkID = string(types.GetDefaultDomain())
	} else {
		// we have some custom network id, overriding default domain
		types.SetDefaultDomain([]byte(cfg.P2pNetworkConfig.NetworkID))
	}
	eth2Network := beaconprotocol.NewNetwork(core.NetworkFromString(cfg.ETH2Options.Network), cfg.ETH2Options.MinGenesisTime)

	currentEpoch := slots.EpochsSinceGenesis(time.Unix(int64(eth2Network.MinGenesisTime()), 0))
	forkVersion := forksprotocol.GetCurrentForkVersion(currentEpoch)

	logger.Info("setting ssv network", zap.String("domain", string(types.GetDefaultDomain())),
		zap.String("net-id", cfg.P2pNetworkConfig.NetworkID),
		zap.String("fork", string(forkVersion)))
	return eth2Network, forkVersion
}

func setupP2P(forkVersion forksprotocol.ForkVersion, operatorPubKey string, db basedb.IDb, logger *zap.Logger) network.P2PNetwork {
	istore := ssv_identity.NewIdentityStore(db, logger)
	netPrivKey, err := istore.SetupNetworkKey(cfg.NetworkPrivateKey)
	if err != nil {
		logger.Fatal("failed to setup network private key", zap.Error(err))
	}

	if len(cfg.P2pNetworkConfig.Subnets) == 0 {
		subnets := getNodeSubnets(logger, db, forkVersion, operatorPubKey)
		cfg.P2pNetworkConfig.Subnets = subnets.String()
	}

	cfg.P2pNetworkConfig.NetworkPrivateKey = netPrivKey
	cfg.P2pNetworkConfig.Logger = logger
	cfg.P2pNetworkConfig.ForkVersion = forkVersion
	cfg.P2pNetworkConfig.OperatorID = format.OperatorID(operatorPubKey)

	return p2pv1.New(&cfg.P2pNetworkConfig)
}

func setupNodes(logger *zap.Logger) (beaconprotocol.Beacon, eth1.Client) {
	// consensus client
	cfg.ETH2Options.Logger = logger
	cfg.ETH2Options.Graffiti = []byte("SSV.Network")
	cl, err := goclient.New(cfg.ETH2Options)
	if err != nil {
		logger.Fatal("failed to create beacon go-client", zap.Error(err),
			zap.String("addr", cfg.ETH2Options.BeaconNodeAddr))
	}

	// execution client
	logger.Info("using registry contract address", zap.String("addr", cfg.ETH1Options.RegistryContractAddr), zap.String("abi version", cfg.ETH1Options.AbiVersion.String()))
	if len(cfg.ETH1Options.RegistryContractABI) > 0 {
		logger.Info("using registry contract abi", zap.String("abi", cfg.ETH1Options.RegistryContractABI))
		if err = eth1.LoadABI(cfg.ETH1Options.RegistryContractABI); err != nil {
			logger.Fatal("failed to load ABI JSON", zap.Error(err))
		}
	}
	el, err := goeth.NewEth1Client(goeth.ClientOptions{
		Ctx:                  cfg.ETH2Options.Context,
		Logger:               logger,
		NodeAddr:             cfg.ETH1Options.ETH1Addr,
		ConnectionTimeout:    cfg.ETH1Options.ETH1ConnectionTimeout,
		ContractABI:          eth1.ContractABI(cfg.ETH1Options.AbiVersion),
		RegistryContractAddr: cfg.ETH1Options.RegistryContractAddr,
		AbiVersion:           cfg.ETH1Options.AbiVersion,
	})
	if err != nil {
		logger.Fatal("failed to create eth1 client", zap.Error(err))
	}

	return cl, el
}

func startMetricsHandler(ctx context.Context, logger *zap.Logger, port int, enableProf bool) {
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(ctx, logger, enableProf, operatorNode.(metrics.HealthCheckAgent))
	addr := fmt.Sprintf(":%d", port)
	if err := metricsHandler.Start(http.NewServeMux(), addr); err != nil {
		// TODO: stop node if metrics setup failed?
		logger.Error("failed to start metrics handler", zap.Error(err))
	}
}

// getNodeSubnets reads all shares and calculates the subnets for this node
// note that we'll trigger another update once finished processing registry events
func getNodeSubnets(logger *zap.Logger, db basedb.IDb, ssvForkVersion forksprotocol.ForkVersion, operatorPubKey string) records.Subnets {
	f := forksfactory.NewFork(ssvForkVersion)
	sharesStorage := validator.NewCollection(validator.CollectionOptions{
		DB:     db,
		Logger: logger,
	})
	subnetsMap := make(map[int]bool)
	shares, err := sharesStorage.GetFilteredValidatorShares(func(share *types.SSVShare) bool {
		return !share.Liquidated && share.BelongsToOperator(operatorPubKey)
	})
	if err != nil {
		logger.Warn("could not read validators to bootstrap subnets")
		return nil
	}
	for _, share := range shares {
		subnet := f.ValidatorSubnet(hex.EncodeToString(share.ValidatorPubKey))
		if subnet < 0 {
			continue
		}
		if !subnetsMap[subnet] {
			subnetsMap[subnet] = true
		}
	}
	subnets := make([]byte, f.Subnets())
	for subnet := range subnetsMap {
		subnets[subnet] = 1
	}
	return subnets
}
