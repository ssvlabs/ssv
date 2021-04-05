package cli

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/network/msgqueue"
	"os"

	"github.com/bloxapp/ssv/beacon/prysmgrpc"

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
		nodeID, err := flags.GetNodeIDKeyFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get node ID flag value", zap.Error(err))
		}
		logger := Logger.With(zap.Uint64("node_id", nodeID))

		eth2Network, err := flags.GetNetworkFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get eth2Network flag value", zap.Error(err))
		}
		logger = logger.With(zap.String("eth2Network", string(eth2Network)))

		discoveryType, err := flags.GetDiscoveryFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get val flag value", zap.Error(err))
		}
		logger = logger.With(zap.String("discovery-type", discoveryType))

		consensusType, err := flags.GetConsensusFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get val flag value", zap.Error(err))
		}
		logger = logger.With(zap.String("val", consensusType))

		beaconAddr, err := flags.GetBeaconAddrFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get beacon node address flag value", zap.Error(err))
		}
		logger = logger.With(zap.String("beacon-addr", beaconAddr))

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

		validatorPk := &bls.PublicKey{}
		if err := validatorPk.DeserializeHexStr(validatorKey); err != nil {
			logger.Fatal("failed to decode validator key", zap.Error(err))
		}
		logger = logger.With(zap.String("validator", "0x"+validatorKey[:12]+"..."))

		baseKey := &bls.SecretKey{}
		if err := baseKey.SetHexString(privKey); err != nil {
			logger.Fatal("failed to set hex private key", zap.Error(err))
		}

		beaconClient, err := prysmgrpc.New(cmd.Context(), logger, baseKey, eth2Network, validatorPk.Serialize(), []byte("BloxStaking"), beaconAddr)
		if err != nil {
			logger.Fatal("failed to create beacon client", zap.Error(err))
		}

		cfg := p2p.Config{
			DiscoveryType: discoveryType,
			BootstrapNodeAddr: []string{
				// deployemnt
				"enr:-LK4QETbiRb0mw8HOE_3f92KRisgIH0XZWaThL8MMhQ1egK6XfD77ER1jm1Z9fVRIQEeXAgdEblLqYKtdzqPuUFCGm8Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArCg3GJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
				// ssh
				//"enr:-LK4QG_q3ygTeNs_YgjPK2tpr624X6YVi1156A8teyeeLcnSFNGp0GN0CLeNA7aNz6JN6KW1nAVAsowojpKkH6DW9XEBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAOOJc2VjcDI1NmsxoQLh6LjwnHfAdgoNkGbhixdtFxIrdt1UDwXNNhUpRPLMk4N0Y3CCE4iDdWRwgg-g"
			},
			UdpPort:   12000,
			TcpPort:   13000,
			TopicName: validatorKey,
			HostDNS:   hostDNS,
			//HostAddress:       "127.0.0.1",
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

	RootCmd.AddCommand(startNodeCmd)
}
