package ssvnode

import (
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon/prysmgrpc"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/node"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions           basedb.Options `yaml:"db"`
	SSVOptions          node.Options   `yaml:"ssv"`
	Network             string         `yaml:"Network" env-default:"pyrmont"`
	DiscoveryType       string         `yaml:"DiscoveryType" env-default:"mdns"`
	BeaconNodeAddr      string         `yaml:"BeaconNodeAddr" env-required:"true"`
	TCPPort             int            `yaml:"TcpPort" env-default:"13000"`
	UDPPort             int            `yaml:"UdpPort" env-default:"12000"`
	HostAddress         string         `yaml:"HostAddress" env:"HOST_ADDRESS" env-required:"true" env-description:"External ip node is exposed for discovery"`
	HostDNS             string         `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`
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
		//beaconAddr, err := flags.GetBeaconAddrFlagValue(cmd)
		//if err != nil {
		//	Logger.Fatal("failed to get beacon node address flag value", zap.Error(err))
		//}
		beaconAddr := cfg.BeaconNodeAddr
		//nodeID, err := flags.GetNodeIDKeyFlagValue(cmd)
		//if err != nil {
		//	Logger.Fatal("failed to get node ID flag value", zap.Error(err))
		//}
		//logger := Logger.With(zap.Uint64("node_id", nodeID))

		//eth2Network, err := flags.GetNetworkFlagValue(cmd)
		//if err != nil {
		//	Logger.Fatal("failed to get eth2Network flag value", zap.Error(err))
		//}
		eth2Network := core.NetworkFromString(cfg.Network)
		beaconClient, err := prysmgrpc.New(cmd.Context(), Logger, eth2Network, []byte("BloxStaking"), beaconAddr)
		if err != nil {
			Logger.Fatal("failed to create beacon client", zap.Error(err))
		}
		//discoveryType, err := flags.GetDiscoveryFlagValue(cmd)
		//if err != nil {
		//	logger.Fatal("failed to get val flag value", zap.Error(err))
		//}
		discoveryType := cfg.DiscoveryType
		//hostDNS, err := flags.GetHostDNSFlagValue(cmd)
		//if err != nil {
		//	logger.Fatal("failed to get hostDNS key flag value", zap.Error(err))
		//}

		//hostAddress, err := flags.GetHostAddressFlagValue(cmd)
		//if err != nil {
		//	logger.Fatal("failed to get hostAddress key flag value", zap.Error(err))
		//}

		//tcpPort, err := flags.GetTCPPortFlagValue(cmd)
		//if err != nil {
		//	Logger.Fatal("failed to get tcp port flag value", zap.Error(err))
		//}
		p2pCfg := p2p.Config{
			DiscoveryType: discoveryType,
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
		cfg.SSVOptions.ValidatorOptions.Logger = Logger
		cfg.SSVOptions.ValidatorOptions.Context = ctx
		cfg.SSVOptions.ValidatorOptions.DB = &db
		cfg.SSVOptions.ValidatorOptions.Network = network
		cfg.SSVOptions.ValidatorOptions.Beacon = &beaconClient

		ssvNode := node.New(cfg.SSVOptions)

		//cfg := p2p.Config{
		//	DiscoveryType: discoveryType,
		//	BootstrapNodeAddr: []string{
		//		// deployemnt
		//		// internal ip
		//		//"enr:-LK4QDAmZK-69qRU5q-cxW6BqLwIlWoYH-BoRlX2N7D9rXBlM7OJ9tWRRtryqvCW04geHC_ab8QmWT9QULnT0Tc5S1cBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
		//		//external ip
		//		"enr:-LK4QHVq6HEA2KVnAw593SRMqUOvMGlkP8Jb-qHn4yPLHx--cStvWc38Or2xLcWgDPynVxXPT9NWIEXRzrBUsLmcFkUBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDbUHcyJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
		//		// ssh
		//		//"enr:-LK4QAkFwcROm9CByx3aabpd9Muqxwj8oQeqnr7vm8PAA8l1ZbDWVZTF_bosINKhN4QVRu5eLPtyGCccRPb3yKG2xjcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAOOJc2VjcDI1NmsxoQMCphx1UQ1PkBsdOb-4FRiSWM4JE7HoDarAzOp82SO4s4N0Y3CCE4iDdWRwgg-g",
		//	},
		//	UDPPort:     udpPort,
		//	TCPPort:     tcpPort,
		//	TopicName:   validatorKey,
		//	HostDNS:     hostDNS,
		//	HostAddress: hostAddress,
		//}
		//network, err := p2p.New(cmd.Context(), logger, &cfg)
		//if err != nil {
		//	logger.Fatal("failed to create network", zap.Error(err))
		//}

		//ssvNode := node.New(node.Options{
		//	ValidatorStorage: validatorStorage,
		//	Beacon:           beaconClient,
		//	ETHNetwork:       eth2Network,
		//	Network:          network,
		//	Queue:            msgQ,
		//	Consensus:        consensusType,
		//	IBFT: ibft.New(
		//		ibftStorage,
		//		network,
		//		msgQ,
		//		&proto.InstanceParams{
		//			ConsensusParams: proto.DefaultConsensusParams(),
		//		},
		//	),
		//	Logger:                     logger,
		//	SignatureCollectionTimeout: sigCollectionTimeout,
		//	Phase1TestGenesis:          genesisEpoch,
		//})

		if err := ssvNode.Start(); err != nil {
			Logger.Fatal("failed to start SSV node", zap.Error(err))
		}

		//
		//Logger.Info("Running node with ports", zap.Int("tcp", tcpPort), zap.Int("udp", udpPort))
		//Logger.Info("Running node with genesis epoch", zap.Uint64("epoch", genesisEpoch))
		//logger.Debug("Node params",
		//	zap.String("eth2Network", string(eth2Network)),
		//	zap.String("discovery-type", discoveryType),
		//	zap.String("val", consensusType),
		//	zap.String("beacon-addr", beaconAddr),
		//	zap.String("validator", "0x"+validatorKey[:12]+"..."))
		//

	},
}

//func configureStorage(storagePath string, logger *zap.Logger, validatorPk *bls.PublicKey, shareKey *bls.SecretKey, nodeID uint64) (collections.ValidatorStorage, collections.IbftStorage) {
//
//	db, err := storage.GetStorageFactory(basedb.Options{
//		Type:   "db",
//		Path:   storagePath,
//		Logger: logger,
//	})
//
//	//db, err := kv.New(storagePath, *logger)
//	if err != nil {
//		logger.Fatal("failed to create db!", zap.Error(err))
//	}
//
//	validatorStorage := collections.NewValidator(db, logger)
//	// saves .env validator to storage
//	ibftCommittee := map[uint64]*proto.Node{
//		1: {
//			IbftId: 1,
//			Pk:     _getBytesFromHex(os.Getenv("PUBKEY_NODE_1")),
//		},
//		2: {
//			IbftId: 2,
//			Pk:     _getBytesFromHex(os.Getenv("PUBKEY_NODE_2")),
//		},
//		3: {
//			IbftId: 3,
//			Pk:     _getBytesFromHex(os.Getenv("PUBKEY_NODE_3")),
//		},
//		4: {
//			IbftId: 4,
//			Pk:     _getBytesFromHex(os.Getenv("PUBKEY_NODE_4")),
//		},
//	}
//
//	if err := validatorStorage.LoadFromConfig(nodeID, validatorPk, shareKey, ibftCommittee); err != nil {
//		logger.Error("Failed to load validator share data from config", zap.Error(err))
//	}
//	ibftStorage := collections.NewIbft(db, logger, "attestation")
//	return validatorStorage, ibftStorage
//}

func init() {
	//flags.AddPrivKeyFlag(StartNodeCmd)
	//flags.AddValidatorKeyFlag(StartNodeCmd)
	//flags.AddBeaconAddrFlag(StartNodeCmd)
	//flags.AddNetworkFlag(StartNodeCmd)
	//flags.AddDiscoveryFlag(StartNodeCmd)
	//flags.AddConsensusFlag(StartNodeCmd)
	//flags.AddNodeIDKeyFlag(StartNodeCmd)
	//flags.AddSignatureCollectionTimeFlag(StartNodeCmd)
	//flags.AddHostDNSFlag(StartNodeCmd)
	//flags.AddHostAddressFlag(StartNodeCmd)
	//flags.AddTCPPortFlag(StartNodeCmd)
	//flags.AddUDPPortFlag(StartNodeCmd)
	//flags.AddGenesisEpochFlag(StartNodeCmd)
	//flags.AddStoragePathFlag(StartNodeCmd)

	//RootCmd.AddCommand(startNodeCmd)
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}
