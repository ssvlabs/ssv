package cli

import (
	"encoding/hex"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/cli/flags"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/implementations/day_number_consensus"
	"github.com/bloxapp/ssv/ibft/networker/p2p"
	"github.com/bloxapp/ssv/ibft/types"
	"github.com/bloxapp/ssv/node"
)

// startNodeCmd is the command to start SSV node
var startNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		network, err := flags.GetNetworkFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get network flag value", zap.Error(err))
		}
		logger := Logger.With(zap.String("network", network))

		beaconAddr, err := flags.GetBeaconAddrFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get beacon node address flag value", zap.Error(err))
		}
		logger = Logger.With(zap.String("beacon-addr", beaconAddr))

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
		logger = Logger.With(zap.String("validator", "0x"+validatorKey[:12]+"..."))

		baseKey := &bls.SecretKey{}
		if err := baseKey.SetHexString(privKey); err != nil {
			logger.Fatal("failed to set hex private key", zap.Error(err))
		}

		beaconClient, err := beacon.NewPrysmGRPC(logger, beaconAddr)
		if err != nil {
			logger.Fatal("failed to create beacon client", zap.Error(err))
		}

		peer, err := p2p.New(cmd.Context(), logger, validatorKey)
		if err != nil {
			logger.Fatal("failed to create peer", zap.Error(err))
		}

		ssvNode := node.New(node.Options{
			ValidatorPubKey: validatorKeyBytes,
			PrivateKey:      baseKey,
			Beacon:          beaconClient,
			Network:         core.NetworkFromString(network),
			IBFTInstance: ibft.New(
				logger,
				&types.Node{
					IbftId: 0, // TODO: Number from arguments - ID
					Pk:     baseKey.GetPublicKey().Serialize(),
					Sk:     baseKey.Serialize(),
				},
				peer,
				&day_number_consensus.DayNumberConsensus{
					Id:     uint64(i),      // TODO: Number from arguments - ID
					Leader: uint64(leader), // TODO: Fill
				},
				&types.InstanceParams{
					ConsensusParams: types.DefaultConsensusParams(),
					IbftCommittee:   nodes, // TODO: Fill
				},
			),
			Logger: logger,
		})

		if err := ssvNode.Start(cmd.Context()); err != nil {
			logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	flags.AddPrivKeyFlag(startNodeCmd)
	flags.AddValidatorKeyFlag(startNodeCmd)
	flags.AddBeaconAddrFlag(startNodeCmd)
	flags.AddNetworkFlag(startNodeCmd)

	RootCmd.AddCommand(startNodeCmd)
}
