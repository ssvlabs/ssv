package ssvnode

import (
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon/prysmgrpc"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/node"
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
	Network                    string         `yaml:"Network" env-default:"pyrmont"`
	DiscoveryType              string         `yaml:"DiscoveryType" env:"DISCOVERY_TYPE_KEY" env-description:"Method to use in discovery" env-default:"mdns"`
	BeaconNodeAddr             string         `yaml:"BeaconNodeAddr" env-required:"true"`
	OperatorKey                string         `yaml:"OperatorKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
	ETH1Addr                   string         `yaml:"ETH1Addr" env-required:"true"`
	TCPPort                    int            `yaml:"TcpPort" env-default:"13000"`
	UDPPort                    int            `yaml:"UdpPort" env-default:"12000"`
	HostAddress                string         `yaml:"HostAddress" env:"HOST_ADDRESS" env-required:"true" env-description:"External ip node is exposed for discovery"`
	HostDNS                    string         `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`
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
		p2pCfg := p2p.Config{
			DiscoveryType: cfg.DiscoveryType,
			BootstrapNodeAddr: []string{
				// deployemnt
				// internal ip
				//"enr:-LK4QDAmZK-69qRU5q-cxW6BqLwIlWoYH-BoRlX2N7D9rXBlM7OJ9tWRRtryqvCW04geHC_ab8QmWT9QULnT0Tc5S1cBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
				//external ip
				"enr:-LK4QHVq6HEA2KVnAw593SRMqUOvMGlkP8Jb-qHn4yPLHx--cStvWc38Or2xLcWgDPynVxXPT9NWIEXRzrBUsLmcFkUBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDbUHcyJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
				// ssh
				//"enr:-LK4QAkFwcROm9CByx3aabpd9Muqxwj8oQeqnr7vm8PAA8l1ZbDWVZTF_bosINKhN4QVRu5eLPtyGCccRPb3yKG2xjcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAOOJc2VjcDI1NmsxoQMCphx1UQ1PkBsdOb-4FRiSWM4JE7HoDarAzOp82SO4s4N0Y3CCE4iDdWRwgg-g",
			},
			UDPPort:     cfg.UDPPort,
			TCPPort:     cfg.TCPPort,
			HostDNS:     cfg.HostDNS,
			HostAddress: cfg.HostAddress,
		}
		network, err := p2p.New(cmd.Context(), Logger, &p2pCfg)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}

		// end Non refactored

		ctx := cmd.Context()
		cfg.SSVOptions.Context = ctx
		cfg.SSVOptions.Logger = Logger
		cfg.SSVOptions.Beacon = &beaconClient
		cfg.SSVOptions.ETHNetwork = &eth2Network
		cfg.SSVOptions.ValidatorOptions.ETHNetwork = &eth2Network
		cfg.SSVOptions.ValidatorOptions.Logger = Logger
		cfg.SSVOptions.ValidatorOptions.Context = ctx
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = network
		cfg.SSVOptions.ValidatorOptions.Beacon = beaconClient

		if cfg.ETH1Addr != "" {
			operatorStorage := collections.NewOperatorStorage(db, Logger)
			if err := operatorStorage.SetupPrivateKey(cfg.OperatorKey); err != nil {
				Logger.Fatal("failed to setup operator private key", zap.Error(err))
			}
			// create new eth1 client
			cfg.SSVOptions.ValidatorOptions.Eth1Client, err = goeth.New(cmd.Context(), Logger, cfg.ETH1Addr, operatorStorage)
			if err != nil {
				Logger.Error("failed to create eth1 client", zap.Error(err)) // TODO change to fatal when times comes
			}
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
