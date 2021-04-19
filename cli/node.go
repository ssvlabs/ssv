package cli

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/beacon/prysmgrpc"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/logex"
	"log"
	"os"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/cli/flags"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/node"
	"github.com/bloxapp/ssv/storage/inmem"
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

		Logger = logex.Build(cmd.Short, loggerLevel)

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

		validatorPk := &bls.PublicKey{}
		if err := validatorPk.DeserializeHexStr(validatorKey); err != nil {
			logger.Fatal("failed to decode validator key", zap.Error(err))
		}

		baseKey := &bls.SecretKey{}
		if err := baseKey.SetHexString(privKey); err != nil {
			logger.Fatal("failed to set hex private key", zap.Error(err))
		}

		beaconClient, err := prysmgrpc.New(cmd.Context(), logger, baseKey, eth2Network, validatorPk.Serialize(), []byte("BloxStaking"), beaconAddr)
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
			UDPPort:      udpPort,
			TCPPort:      tcpPort,
			TopicName:    validatorKey,
			HostDNS:      hostDNS,
			HostAddress:  hostAddress,
		}
		network, err := p2p.New(cmd.Context(), logger, &cfg)
		if err != nil {
			logger.Fatal("failed to create network", zap.Error(err))
		}

		// TODO: Refactor that
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

		ibftCommittee[nodeID].Pk = baseKey.GetPublicKey().Serialize()
		ibftCommittee[nodeID].Sk = baseKey.Serialize()

		//for id, obj := range ibftCommittee{
		//	if len(obj.Pk) == 0 {
		//		errors.New(fmt.Sprint("Missing public key for node index - %v", id))
		//	}
		//}

		msgQ := msgqueue.New()

		ssvNode := node.New(node.Options{
			NodeID:          nodeID,
			ValidatorPubKey: validatorPk,
			PrivateKey:      baseKey,
			Beacon:          beaconClient,
			ETHNetwork:      eth2Network,
			Network:         network,
			Queue:           msgQ,
			Consensus:       consensusType,
			IBFT: ibft.New(
				inmem.New(),
				&proto.Node{
					IbftId: nodeID,
					Pk:     baseKey.GetPublicKey().Serialize(),
					Sk:     baseKey.Serialize(),
				},
				network,
				msgQ,
				&proto.InstanceParams{
					ConsensusParams: proto.DefaultConsensusParams(),
					IbftCommittee:   ibftCommittee,
				},
			),
			Logger:                     logger,
			SignatureCollectionTimeout: sigCollectionTimeout,
			Phase1TestGenesis:          genesisEpoch,
		})

		if err := ssvNode.Start(cmd.Context()); err != nil {
			logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
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
	flags.AddLoggerLevelFlag(RootCmd)

	RootCmd.AddCommand(startNodeCmd)
}
