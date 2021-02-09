package cli

import (
	"encoding/hex"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon"
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

		leaderID, err := flags.GetLeaderIDKeyFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get leader ID flag value", zap.Error(err))
		}
		logger = logger.With(zap.Uint64("leader_id", leaderID))

		network, err := flags.GetNetworkFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get network flag value", zap.Error(err))
		}
		logger = logger.With(zap.String("network", string(network)))

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

		validatorKeyBytes, err := hex.DecodeString(validatorKey)
		if err != nil {
			logger.Fatal("failed to decode validator key", zap.Error(err))
		}
		logger = logger.With(zap.String("validator", "0x"+validatorKey[:12]+"..."))

		baseKey := &bls.SecretKey{}
		if err := baseKey.SetHexString(privKey); err != nil {
			logger.Fatal("failed to set hex private key", zap.Error(err))
		}

		beaconClient, err := beacon.NewPrysmGRPC(logger, baseKey, network, validatorKeyBytes, []byte("BloxStaking"), beaconAddr)
		if err != nil {
			logger.Fatal("failed to create beacon client", zap.Error(err))
		}

		peer, err := p2p.New(cmd.Context(), logger, validatorKey)
		if err != nil {
			logger.Fatal("failed to create peer", zap.Error(err))
		}

		// TODO: Refactor that
		ibftCommittee := map[uint64]*proto.Node{
			1: {
				IbftId: 1,
				Pk:     _getBytesFromHex("89791f38daa4ba0288035e6e12f11dd7c6e68651f2ae81f40a5bc5002e32281cab7b16e0ce12084619a2ce3d23d9f357"),
			},
			2: {
				IbftId: 2,
				Pk:     _getBytesFromHex("98090f5ace3818b27b6ea2d38524556ec8c39d568a1595adb00964415eae62998c12ef015eda17f8d590f22b1778ec02"),
			},
			3: {
				IbftId: 3,
				Pk:     _getBytesFromHex("9492358822d8ac4e678a2aa500b7634495ef9efece3a787cd48efe97180f95dda537eaf34d412eb2c42ab2dcf9e1d165"),
			},
		}
		ibftCommittee[nodeID].Pk = baseKey.GetPublicKey().Serialize()
		ibftCommittee[nodeID].Sk = baseKey.Serialize()

		ssvNode := node.New(node.Options{
			NodeID:          nodeID,
			ValidatorPubKey: validatorKeyBytes,
			PrivateKey:      baseKey,
			Beacon:          beaconClient,
			ETHNetwork:      network,
			Network:         peer,
			Consensus:       consensusType,
			IBFT: ibft.New(
				inmem.New(),
				&proto.Node{
					IbftId: nodeID,
					Pk:     baseKey.GetPublicKey().Serialize(),
					Sk:     baseKey.Serialize(),
				},
				peer,
				&proto.InstanceParams{
					ConsensusParams: proto.DefaultConsensusParams(),
					IbftCommittee:   ibftCommittee,
				},
			),
			Logger: logger,
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
	flags.AddConsensusFlag(startNodeCmd)
	flags.AddNodeIDKeyFlag(startNodeCmd)
	flags.AddLeaderIDKeyFlag(startNodeCmd)

	RootCmd.AddCommand(startNodeCmd)
}
