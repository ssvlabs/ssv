package operator

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/beacon/goclient"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/migrations"
	"github.com/bloxapp/ssv/monitoring/metrics"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/operator"
	"github.com/bloxapp/ssv/operator/duties"
	v0 "github.com/bloxapp/ssv/operator/forks/v0"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/validator"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options   `yaml:"db"`
	SSVOptions                 operator.Options `yaml:"ssv"`
	ETH1Options                eth1.Options     `yaml:"eth1"`
	ETH2Options                beacon.Options   `yaml:"eth2"`
	P2pNetworkConfig           p2p.Config       `yaml:"p2p"`

	OperatorPrivateKey         string `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
	GenerateOperatorPrivateKey bool   `yaml:"GenerateOperatorPrivateKey" env:"GENERATE_OPERATOR_KEY" env-description:"Whether to generate operator key if none is passed by config"`
	MetricsAPIPort             int    `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"port of metrics api"`
	EnableProfile              bool   `yaml:"EnableProfile" env:"ENABLE_PROFILE" env-description:"flag that indicates whether go profiling tools are enabled"`
	NetworkPrivateKey          string `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"private key for network identity"`

	ReadOnlyMode bool `yaml:"ReadOnlyMode" env:"READ_ONLY_MODE" env-description:"a flag to turn on read only operator"`
}

var cfg config

var globalArgs global_config.Args

var operatorNode operator.Node

// StartNodeCmd is the command to start SSV node
var StartNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)
		log.Printf("starting %s", commons.GetBuildData())
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}
		if globalArgs.ShareConfigPath != "" {
			if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
				log.Fatal(err)
			}
		}
		loggerLevel, errLogLevel := logex.GetLoggerLevelValue(cfg.LogLevel)
		Logger := logex.Build(commons.GetBuildData(), loggerLevel, &logex.EncodingConfig{
			Format:       cfg.GlobalConfig.LogFormat,
			LevelEncoder: logex.LevelEncoder([]byte(cfg.LogLevelFormat)),
		})
		if errLogLevel != nil {
			Logger.Warn(fmt.Sprintf("Default log level set to %s", loggerLevel), zap.Error(errLogLevel))
		}

		// TODO - change via command line?
		fork := v0.New()

		cfg.DBOptions.Logger = Logger
		cfg.DBOptions.Ctx = cmd.Context()
		db, err := storage.GetStorageFactory(cfg.DBOptions)
		if err != nil {
			Logger.Fatal("failed to create db!", zap.Error(err))
		}

		migrationOpts := migrations.Options{
			Db:     db,
			Logger: Logger,
			DbPath: cfg.DBOptions.Path,
		}
		err = migrations.Run(cmd.Context(), migrationOpts)
		if err != nil {
			Logger.Fatal("failed to run migrations", zap.Error(err))
		}

		eth2Network := core.NetworkFromString(cfg.ETH2Options.Network)

		// TODO Not refactored yet Start (refactor in exporter as well):
		cfg.ETH2Options.Context = cmd.Context()
		cfg.ETH2Options.Logger = Logger
		cfg.ETH2Options.Graffiti = []byte("SSV.Network")
		cfg.ETH2Options.DB = db
		beaconClient, err := goclient.New(cfg.ETH2Options)
		if err != nil {
			Logger.Fatal("failed to create beacon go-client", zap.Error(err),
				zap.String("addr", cfg.ETH2Options.BeaconNodeAddr))
		}

		operatorStorage := operator.NewOperatorNodeStorage(db, Logger)
		if err := operatorStorage.SetupPrivateKey(cfg.GenerateOperatorPrivateKey, cfg.OperatorPrivateKey); err != nil {
			Logger.Fatal("failed to setup operator private key", zap.Error(err))
		}
		operatorPrivKey, found, err := operatorStorage.GetPrivateKey()
		if err != nil || !found {
			Logger.Fatal("failed to get operator private key", zap.Error(err))
		}
		cfg.P2pNetworkConfig.OperatorPrivateKey = operatorPrivKey

		p2pStorage := p2p.NewP2PStorage(db, Logger) // TODO might need to move to separate storage
		if err := p2pStorage.SetupPrivateKey(cfg.NetworkPrivateKey); err != nil {
			Logger.Fatal("failed to setup p2p private key", zap.Error(err))
		}
		p2pPrivKey, found, err := p2pStorage.GetPrivateKey()
		if err != nil || !found {
			Logger.Fatal("failed to get p2p private key", zap.Error(err))
		}
		cfg.P2pNetworkConfig.NetworkPrivateKey = p2pPrivKey
		cfg.P2pNetworkConfig.Fork = fork.NetworkFork()
		cfg.P2pNetworkConfig.NodeType = p2p.Operator
		p2pNet, err := p2p.New(cmd.Context(), Logger, &cfg.P2pNetworkConfig)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}

		ctx := cmd.Context()
		cfg.SSVOptions.Fork = fork
		cfg.SSVOptions.Context = ctx
		cfg.SSVOptions.Logger = Logger
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.Beacon = beaconClient
		cfg.SSVOptions.ETHNetwork = &eth2Network
		cfg.SSVOptions.Network = p2pNet

		cfg.SSVOptions.UseMainTopic = cfg.P2pNetworkConfig.UseMainTopic

		cfg.SSVOptions.ValidatorOptions.Fork = cfg.SSVOptions.Fork
		cfg.SSVOptions.ValidatorOptions.ETHNetwork = &eth2Network
		cfg.SSVOptions.ValidatorOptions.Logger = Logger
		cfg.SSVOptions.ValidatorOptions.Context = ctx
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = p2pNet
		cfg.SSVOptions.ValidatorOptions.Beacon = beaconClient // TODO need to be pointer?
		cfg.SSVOptions.ValidatorOptions.CleanRegistryData = cfg.ETH1Options.CleanRegistryData
		cfg.SSVOptions.ValidatorOptions.KeyManager = beaconClient

		cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider = operatorStorage.GetPrivateKey

		Logger.Info("using registry contract address", zap.String("addr", cfg.ETH1Options.RegistryContractAddr), zap.String("abi version", cfg.ETH1Options.AbiVersion.String()))

		// create new eth1 client
		if len(cfg.ETH1Options.RegistryContractABI) > 0 {
			Logger.Info("using registry contract abi", zap.String("abi", cfg.ETH1Options.RegistryContractABI))
			if err = eth1.LoadABI(cfg.ETH1Options.RegistryContractABI); err != nil {
				Logger.Fatal("failed to load ABI JSON", zap.Error(err))
			}
		}
		cfg.SSVOptions.Eth1Client, err = goeth.NewEth1Client(goeth.ClientOptions{
			Ctx:                        cmd.Context(),
			Logger:                     Logger,
			NodeAddr:                   cfg.ETH1Options.ETH1Addr,
			ConnectionTimeout:          cfg.ETH1Options.ETH1ConnectionTimeout,
			ContractABI:                eth1.ContractABI(cfg.ETH1Options.AbiVersion),
			RegistryContractAddr:       cfg.ETH1Options.RegistryContractAddr,
			ShareEncryptionKeyProvider: operatorStorage.GetPrivateKey,
			AbiVersion:                 cfg.ETH1Options.AbiVersion,
		})
		if err != nil {
			Logger.Fatal("failed to create eth1 client", zap.Error(err))
		}

		validatorCtrl := validator.NewController(cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl
		if cfg.ReadOnlyMode {
			cfg.SSVOptions.DutyExec = duties.NewReadOnlyExecutor(Logger)
		}
		operatorNode = operator.New(cfg.SSVOptions)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(cmd.Context(), Logger, cfg.MetricsAPIPort, cfg.EnableProfile)
		}

		metrics.WaitUntilHealthy(Logger, cfg.SSVOptions.Eth1Client, "eth1 node")
		metrics.WaitUntilHealthy(Logger, beaconClient, "beacon node")

		if err := operatorNode.StartEth1(eth1.HexStringToSyncOffset(cfg.ETH1Options.ETH1SyncOffset)); err != nil {
			Logger.Fatal("failed to start eth1", zap.Error(err))
		}
		if err := operatorNode.Start(); err != nil {
			Logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
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
