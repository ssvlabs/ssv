package cli

import (
	"encoding/hex"

	"github.com/bloxapp/ssv/ibft/proto"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/cli/flags"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/node"
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
			logger.Fatal("failed to get valueImpl flag value", zap.Error(err))
		}
		logger = logger.With(zap.String("valueImpl", consensusType))

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

		beaconClient, err := beacon.NewPrysmGRPC(logger, baseKey, network, validatorKeyBytes, beaconAddr)
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
				Pk:     _getBytesFromHex("a50a268aca7be24d4032cd0295db234b3196ed1a0dcd7368b71df73521472db96edf2c822f015cb80f426d4bce5c1229"),
			},
			2: {
				IbftId: 2,
				Pk:     _getBytesFromHex("8e075489434c0f7c246c555dba372e8acf3ca55d50652fc2eccd9a2261c54c8fa84873abbc4983acdb4a75e2a4c50db5"),
			},
			3: {
				IbftId: 3,
				Pk:     _getBytesFromHex("8e0bc250eb11f80bf57aef6d55d332f3253d01b1a56cb5d75b58d9680abe227b06c82be94891f9d3d32ed3fc60e36b55"),
			},
		}
		ibftCommittee[nodeID].Pk = baseKey.GetPublicKey().Serialize()
		ibftCommittee[nodeID].Sk = baseKey.Serialize()

		ssvNode := node.New(node.Options{
			ValidatorPubKey: validatorKeyBytes,
			PrivateKey:      baseKey,
			Beacon:          beaconClient,
			Network:         network,
			Consensus:       consensusType,
			IBFT: ibft.New(
				nil, // TODO: Implement DB
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
