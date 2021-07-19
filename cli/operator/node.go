package operator

import (
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/beacon/goclient"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/metrics"
	metrics_ps "github.com/bloxapp/ssv/metrics/process"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/operator"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/validator"
	metrics_validator "github.com/bloxapp/ssv/validator/metrics"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
	"net/http"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options   `yaml:"db"`
	SSVOptions                 operator.Options `yaml:"ssv"`
	ETH1Options                eth1.Options     `yaml:"eth1"`
	ETH2Options                beacon.Options   `yaml:"eth2"`
	P2pNetworkConfig           p2p.Config       `yaml:"p2p"`

	MetricsAPIPort     int    `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"port of metrics api"`
	OperatorPrivateKey string `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
}

var cfg config

var globalArgs global_config.Args

// StartNodeCmd is the command to start SSV node
var StartNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		log.Printf("starting %s:%s", cmd.Parent().Short, cmd.Parent().Version)

		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}
		if globalArgs.ShareConfigPath != "" {
			if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
				log.Fatal(err)
			}
		}
		loggerLevel, err := logex.GetLoggerLevelValue(cfg.LogLevel)
		Logger := logex.Build(cmd.Parent().Short, loggerLevel, cfg.GlobalConfig.LogFormat)

		if err != nil {
			Logger.Warn(fmt.Sprintf("Default log level set to %s", loggerLevel), zap.Error(err))
		}
		cfg.DBOptions.Logger = Logger
		db, err := storage.GetStorageFactory(cfg.DBOptions)
		if err != nil {
			Logger.Fatal("failed to create db!", zap.Error(err))
		}

		eth2Network := core.NetworkFromString(cfg.ETH2Options.Network)

		// TODO Not refactored yet Start:
		cfg.ETH2Options.Context = cmd.Context()
		cfg.ETH2Options.Logger = Logger
		cfg.ETH2Options.Graffiti = []byte("BloxStaking")
		beaconClient, err := goclient.New(cfg.ETH2Options)
		if err != nil {
			Logger.Fatal("failed to create beacon go-client", zap.Error(err))
		}

		network, err := p2p.New(cmd.Context(), Logger, &cfg.P2pNetworkConfig)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}

		ctx := cmd.Context()
		cfg.SSVOptions.Context = ctx
		cfg.SSVOptions.Logger = Logger
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.Beacon = &beaconClient
		cfg.SSVOptions.ETHNetwork = &eth2Network

		cfg.SSVOptions.ValidatorOptions.ETHNetwork = &eth2Network
		cfg.SSVOptions.ValidatorOptions.Logger = Logger
		cfg.SSVOptions.ValidatorOptions.Context = ctx
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = network
		cfg.SSVOptions.ValidatorOptions.Beacon = beaconClient // TODO need to be pointer?

		operatorStorage := operator.NewOperatorNodeStorage(db, Logger)
		if err := operatorStorage.SetupPrivateKey(cfg.OperatorPrivateKey); err != nil {
			Logger.Fatal("failed to setup operator private key", zap.Error(err))
		}
		cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider = operatorStorage.GetPrivateKey

		// create new eth1 client
		Logger.Info("using registry contract address", zap.String("addr", cfg.ETH1Options.RegistryContractAddr))
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
			ContractABI:                eth1.ContractABI(),
			RegistryContractAddr:       cfg.ETH1Options.RegistryContractAddr,
			ShareEncryptionKeyProvider: operatorStorage.GetPrivateKey,
		})
		if err != nil {
			Logger.Fatal("failed to create eth1 client", zap.Error(err))
		}

		slotQueue := slotqueue.New(*cfg.SSVOptions.ETHNetwork, Logger)
		cfg.SSVOptions.SlotQueue = slotQueue
		cfg.SSVOptions.ValidatorOptions.SlotQueue = slotQueue
		validatorCtrl := validator.NewController(cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl

		operatorNode := operator.New(cfg.SSVOptions)

		if err := operatorNode.StartEth1(eth1.HexStringToSyncOffset(cfg.ETH1Options.ETH1SyncOffset)); err != nil {
			Logger.Fatal("failed to start eth1", zap.Error(err))
		}
		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(Logger, cfg.MetricsAPIPort)
		}
		if err := operatorNode.Start(); err != nil {
			Logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}

func startMetricsHandler(logger *zap.Logger, port int) {
	// register process metrics
	metrics_ps.SetupProcessMetrics()
	p2p.SetupNetworkMetrics(logger, cfg.SSVOptions.ValidatorOptions.Network)
	metrics_validator.SetupMetricsCollector(logger, cfg.SSVOptions.ValidatorController, cfg.SSVOptions.ValidatorOptions.Network)
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(logger)
	addr := fmt.Sprintf(":%d", port)
	logger.Info("starting metrics handler", zap.String("addr", addr))
	if err := metricsHandler.Start(http.NewServeMux(), addr); err != nil {
		// TODO: stop node if metrics setup failed?
		logger.Error("failed to start metrics handler", zap.Error(err))
	}
}
