package cli

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/beacon/prysmgrpc"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/logex"
	"log"
	"os"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/cli/flags"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/node"
)

// startNodeCmd is the command to start SSV node
var startNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		loggerLevel, err := flags.GetLoggerLevelValue(RootCmd)
		if err != nil {
			log.Fatal("failed to get logger level flag value", zap.Error(err))
		}

		Logger = logex.Build(RootCmd.Short, loggerLevel)

		nodeID, err := flags.GetNodeIDKeyFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get node ID flag value", zap.Error(err))
		}
		logger := Logger.With(zap.Uint64("node_id", nodeID))

		eth2Network, err := flags.GetNetworkFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get eth2Network flag value", zap.Error(err))
		}

		discoveryType, err := flags.GetDiscoveryFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get val flag value", zap.Error(err))
		}

		consensusType, err := flags.GetConsensusFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get val flag value", zap.Error(err))
		}

		beaconAddr, err := flags.GetBeaconAddrFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get beacon node address flag value", zap.Error(err))
		}

		privKey, err := flags.GetPrivKeyFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get private key flag value", zap.Error(err))
		}

		validatorKey, err := flags.GetValidatorKeyFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get validator public key flag value", zap.Error(err))
		}

		sigCollectionTimeout, err := flags.GetSignatureCollectionTimeValue(cmd)
		if err != nil {
			logger.Fatal("failed to get signature timeout key flag value", zap.Error(err))
		}

		hostDNS, err := flags.GetHostDNSFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get hostDNS key flag value", zap.Error(err))
		}

		hostAddress, err := flags.GetHostAddressFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get hostAddress key flag value", zap.Error(err))
		}

		tcpPort, err := flags.GetTCPPortFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get tcp port flag value", zap.Error(err))
		}
		udpPort, err := flags.GetUDPPortFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get udp port flag value", zap.Error(err))
		}

		genesisEpoch, err := flags.GetGenesisEpochValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get genesis epoch flag value", zap.Error(err))
		}

		storagePath, err := flags.GetStoragePathValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get storage path flag value", zap.Error(err))
		}

		validatorPk := &bls.PublicKey{}
		if err := validatorPk.DeserializeHexStr(validatorKey); err != nil {
			logger.Fatal("failed to decode validator key", zap.Error(err))
		}

		shareKey := &bls.SecretKey{}
		if err := shareKey.SetHexString(privKey); err != nil {
			logger.Fatal("failed to set hex private key", zap.Error(err))
		}

		// init storage
		validatorStorage, ibftStorage := configureStorage(storagePath, logger, validatorPk, shareKey, nodeID)

		beaconClient, err := prysmgrpc.New(cmd.Context(), logger, eth2Network, []byte("BloxStaking"), beaconAddr)
		if err != nil {
			logger.Fatal("failed to create beacon client", zap.Error(err))
		}

		Logger.Info("Running node with ports", zap.Int("tcp", tcpPort), zap.Int("udp", udpPort))
		Logger.Info("Running node with genesis epoch", zap.Uint64("epoch", genesisEpoch))
		logger.Debug("Node params",
			zap.String("eth2Network", string(eth2Network)),
			zap.String("discovery-type", discoveryType),
			zap.String("val", consensusType),
			zap.String("beacon-addr", beaconAddr),
			zap.String("validator", "0x"+validatorKey[:12]+"..."))

		cfg := p2p.Config{
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
			UDPPort:     udpPort,
			TCPPort:     tcpPort,
			HostDNS:     hostDNS,
			HostAddress: hostAddress,
		}
		network, err := p2p.New(cmd.Context(), logger, validatorStorage, &cfg)
		if err != nil {
			logger.Fatal("failed to create network", zap.Error(err))
		}

		ssvNode := node.New(node.Options{
			ValidatorStorage: validatorStorage,
			IbftStorage:      ibftStorage,
			Beacon:           beaconClient,
			ETHNetwork:       eth2Network,
			Network:          network,
			Consensus:        consensusType,
			Logger:                     logger,
			SignatureCollectionTimeout: sigCollectionTimeout,
			Phase1TestGenesis:          genesisEpoch,
		})

		if err := ssvNode.Start(cmd.Context()); err != nil {
			logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func configureStorage(storagePath string, logger *zap.Logger, validatorPk *bls.PublicKey, shareKey *bls.SecretKey, nodeID uint64) (collections.ValidatorStorage, collections.IbftStorage) {
	db, err := kv.New(storagePath, *logger)
	if err != nil {
		logger.Fatal("failed to create db!", zap.Error(err))
	}

	validatorStorage := collections.NewValidator(db, logger)
	// saves .env validator to storage
	ibftCommittee := map[uint64]*proto.Node{
		1: {
			IbftId: 1,
			Pk:     _getBytesFromHex(os.Getenv("PUBKEY_NODE_1")),
		},
		2: {
			IbftId: 2,
			Pk:     _getBytesFromHex(os.Getenv("PUBKEY_NODE_2")),
		},
		3: {
			IbftId: 3,
			Pk:     _getBytesFromHex(os.Getenv("PUBKEY_NODE_3")),
		},
		4: {
			IbftId: 4,
			Pk:     _getBytesFromHex(os.Getenv("PUBKEY_NODE_4")),
		},
	}


	if err := validatorStorage.LoadFromConfig(nodeID, validatorPk, shareKey, ibftCommittee); err != nil {
		logger.Error("Failed to load validator share data from config", zap.Error(err))
	}

	secValidator(&validatorStorage, logger, nodeID)

	ibftStorage := collections.NewIbft(db, logger)
	return validatorStorage, ibftStorage
}

func secValidator(validatorStorage *collections.ValidatorStorage, logger *zap.Logger, nodeID uint64) {
	validatorKey := "b4dbeb61df8d810ff7c3d319716c867ac8d717ac6037413e66f543ec031fd23f10d58cf0f94831ec4963b9dc6d6555ad"
	pubKey2 := &bls.PublicKey{}
	if err := pubKey2.DeserializeHexStr(validatorKey); err != nil{
		logger.Error("failed to deserialize pubkey 2",zap.Error(err))
	}
	secrets := make(map[uint64]string)
	secrets[1] = "6cef8480cc608b428805961b67d8014fbad50dc8863fde24b95acc51b06ea7c4"
	secrets[2] = "229b3da8e937c2c89a9406256e3852a86ad390ede5b26c89f7042db18536e31f"
	secrets[3] = "4ebde2ca1f7c1ff3a45a76434ff325d68191ddac4d474c66f0b3dfc7561ceca7"
	secrets[4] = "097c253e1bf2a8333ee53664f9c4cacf5794abfdbd01c5bda669e2952320c45a"
	secert2:= &bls.SecretKey{}
	if err := secert2.SetHexString(secrets[nodeID]); err != nil{
		logger.Error("failed to deserialize secret 2",zap.Error(err))
	}

	ibftCommittee2 := map[uint64]*proto.Node{
		1: {
			IbftId: 1,
			Pk:     _getBytesFromHex("a07ade519fe96253491fdef133c2a0368ca16fb9fbba385fd3ee1818d3872c30b05d831c16a0e76150b01d0cf61caf3c"),
		},
		2: {
			IbftId: 2,
			Pk:     _getBytesFromHex("ab95d4889e37868c3e37d849455f01d0bf7d689258ab243480e32b4fac3e68ec4564518f03481ba4694604ff8814cffe"),
		},
		3: {
			IbftId: 3,
			Pk:     _getBytesFromHex("b50727d4272969ed51a5de6eca8ffecb0feb55dc5865e9b1a6335f4e80b680757affc08ad06eba0123b68779f7b6d782"),
		},
		4: {
			IbftId: 4,
			Pk:     _getBytesFromHex("8b28bd9c4d57928e5c67969ea850d2fd14e5f222ce7333e477a9ebd2be0e3c2df3d46b93c30080231dea30dbcf8836fe"),
		},
	}
	ibftCommittee2[nodeID].Pk = secert2.GetPublicKey().Serialize()
	ibftCommittee2[nodeID].Sk = secert2.Serialize()

	val2 := collections.Validator{
		NodeID:     nodeID,
		PubKey:     pubKey2,
		ShareKey:   secert2,
		Committiee: ibftCommittee2,
	}

	if err := validatorStorage.SaveValidatorShare(&val2); err != nil{
		logger.Error("failed to save validator 2",zap.Error(err))
	}else {
		logger.Info("save validator 2 success")
	}
}

func _getBytesFromHex(str string) []byte {
	val, _ := hex.DecodeString(str)
	return val
}

func init() {
	flags.AddPrivKeyFlag(startNodeCmd)
	flags.AddValidatorKeyFlag(startNodeCmd)
	flags.AddBeaconAddrFlag(startNodeCmd)
	flags.AddNetworkFlag(startNodeCmd)
	flags.AddDiscoveryFlag(startNodeCmd)
	flags.AddConsensusFlag(startNodeCmd)
	flags.AddNodeIDKeyFlag(startNodeCmd)
	flags.AddSignatureCollectionTimeFlag(startNodeCmd)
	flags.AddHostDNSFlag(startNodeCmd)
	flags.AddHostAddressFlag(startNodeCmd)
	flags.AddTCPPortFlag(startNodeCmd)
	flags.AddUDPPortFlag(startNodeCmd)
	flags.AddGenesisEpochFlag(startNodeCmd)
	flags.AddStoragePathFlag(startNodeCmd)
	flags.AddLoggerLevelFlag(RootCmd)

	RootCmd.AddCommand(startNodeCmd)
}
