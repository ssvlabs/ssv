package operator

import (
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon/prysmgrpc"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/metrics"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/operator"
	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/validator"
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

	Network           string `yaml:"Network" env:"NETWORK" env-default:"prater"`
	BeaconNodeAddr    string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true"`
	OperatorKey       string `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
	ETH1Addr          string `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true"`
	ETH1SyncOffset    string `yaml:"ETH1SyncOffset" env:"ETH_1_SYNC_OFFSET"`
	SmartContractAddr string `yaml:"SmartContractAddr" env:"SMART_CONTRACT_ADDR_KEY" env-description:"smart contract addr listen to event from" env-default:""`

	P2pNetworkConfig p2p.Config `yaml:"p2p"`
}

var cfg config

var globalArgs global_config.Args

// StartNodeCmd is the command to start SSV node
var StartNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}
		if globalArgs.ShareConfigPath != "" {
			if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
				log.Fatal(err)
			}
		}
		loggerLevel, err := logex.GetLoggerLevelValue(cfg.LogLevel)
		Logger := logex.Build(cmd.Parent().Short, loggerLevel)

		if err != nil {
			Logger.Warn(fmt.Sprintf("Default log level set to %s", loggerLevel), zap.Error(err))
		}
		cfg.DBOptions.Logger = Logger
		db, err := storage.GetStorageFactory(cfg.DBOptions)
		if err != nil {
			Logger.Fatal("failed to create db!", zap.Error(err))
		}

		// TODO Not refactored yet Start:
		eth2Network := core.NetworkFromString(cfg.Network)
		beaconClient, err := prysmgrpc.New(cmd.Context(), Logger, eth2Network, []byte("BloxStaking"), cfg.BeaconNodeAddr)
		if err != nil {
			Logger.Fatal("failed to create beacon client", zap.Error(err))
		}

		network, err := p2p.New(cmd.Context(), Logger, &cfg.P2pNetworkConfig)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}
		// end Non refactored

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
		cfg.SSVOptions.ValidatorOptions.Beacon = beaconClient

		operatorStorage := operator.NewOperatorNodeStorage(db, Logger)
		if err := operatorStorage.SetupPrivateKey(cfg.OperatorKey); err != nil {
			Logger.Fatal("failed to setup operator private key", zap.Error(err))
		}
		// create new eth1 client
		if cfg.SmartContractAddr != "" {
			Logger.Info("using smart contract addr from cfg", zap.String("addr", cfg.SmartContractAddr))
			params.SsvConfig().OperatorContractAddress = cfg.SmartContractAddr // TODO need to remove config and use in eth2 option cfg
		}
		cfg.SSVOptions.Eth1Client, err = goeth.NewEth1Client(goeth.ClientOptions{
			Ctx:             cmd.Context(),
			Logger:          Logger,
			NodeAddr:        cfg.ETH1Addr,
			PrivKeyProvider: operatorStorage.GetPrivateKey,
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

		if err := operatorNode.StartEth1(eth1.HexStringToSyncOffset(cfg.ETH1SyncOffset)); err != nil {
			Logger.Fatal("failed to start eth1", zap.Error(err))
		}
		go func() {
			metricsHandler := metrics.NewMetricsHandler(Logger)
			if err := metricsHandler.Start(http.NewServeMux(), ":15000"); err != nil {
				// TODO: stop node if metrics setup failed?
				Logger.Error("failed to start metrics handler", zap.Error(err))
			}
		}()
		if err := operatorNode.Start(); err != nil {
			Logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}
