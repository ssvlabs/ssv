package ssvnode

import (
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon/prysmgrpc"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/node"
	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options `yaml:"db"`
	SSVOptions                 node.Options   `yaml:"ssv"`

	Network                    string         `yaml:"Network" env-default:"prater"`
	BeaconNodeAddr             string         `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true"`
	OperatorKey                string         `yaml:"OperatorKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
	ETH1Addr                   string         `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true"`
	SmartContractAddr          string         `yaml:"SmartContractAddr" env:"SMART_CONTRACT_ADDR_KEY" env-description:"smart contract addr listen to event from" env-default:""`

	P2pNetworkConfig           p2p.Config     `yaml:"p2p"`
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

		operatorStorage := collections.NewOperatorStorage(db, Logger)
		if err := operatorStorage.SetupPrivateKey(cfg.OperatorKey); err != nil {
			Logger.Fatal("failed to setup operator private key", zap.Error(err))
		}
		// create new eth1 client
		if  cfg.SmartContractAddr != "" {
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

		ssvNode := node.New(cfg.SSVOptions)

		if err := ssvNode.Start(); err != nil {
			Logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}
