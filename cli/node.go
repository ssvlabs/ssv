package cli

import (
	"encoding/hex"

	"github.com/bloxapp/ssv/utils/grpcex"
	"github.com/herumi/bls-eth-go-binary/bls"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/cli/flags"
)

// startNodeCmd is the command to start SSV node
var startNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		privKey, err := flags.GetPrivKeyFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get private key flag value", zap.Error(err))
		}

		validatorKey, err := flags.GetValidatorKeyFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get validator key flag value", zap.Error(err))
		}

		validatorKeyBytes, err := hex.DecodeString(validatorKey)
		if err != nil {
			Logger.Fatal("failed to decode validator key", zap.Error(err))
		}

		logger := Logger.With(zap.String("validator", "0x"+validatorKey[:12]+"..."))

		baseKey := &bls.SecretKey{}
		if err := baseKey.SetHexString(privKey); err != nil {
			logger.Fatal("failed to set hex private key", zap.Error(err))
		}

		logger = logger.With(zap.String("threshold", "0x"+baseKey.GetPublicKey().SerializeToHexStr()[:12]+"..."))

		conn, err := grpcex.DialConn("eth2-4000-prysm-ext.stage.bloxinfra.com:80")
		if err != nil {
			logger.Fatal("failed to dial gRPC connection", zap.Error(err))
		}
		validatorClient := ethpb.NewBeaconNodeValidatorClient(conn)

		statusResp, err := validatorClient.ValidatorStatus(cmd.Context(), &ethpb.ValidatorStatusRequest{
			PublicKey: validatorKeyBytes,
		})
		if err != nil {
			logger.Fatal("failed to get validator status", zap.Error(err))
		}
		logger.Info("Validator status", zap.String("status", ethpb.ValidatorStatus_name[int32(statusResp.GetStatus())]))

		streamDuties, err := validatorClient.StreamDuties(cmd.Context(), &ethpb.DutiesRequest{
			PublicKeys: [][]byte{validatorKeyBytes},
		})
		if err != nil {
			logger.Fatal("failed to open duties stream", zap.Error(err))
		}

		logger.Info("start streaming duties")
		for {
			resp, err := streamDuties.Recv()
			if err != nil {
				logger.Fatal("failed to receive duties from stream", zap.Error(err))
			}

			for _, duty := range resp.GetCurrentEpochDuties() {
				logger.Info("Got current epoch duties. Start IBFT instance",
					zap.Uint64("committee-index", duty.GetCommitteeIndex()),
					zap.Uint64("attester-slot", duty.GetAttesterSlot()),
					zap.Uint64s("proposer-slots", duty.GetProposerSlots()))
			}
		}
	},
}

func init() {
	flags.AddPrivKeyFlag(startNodeCmd)
	flags.AddValidatorKeyFlag(startNodeCmd)

	RootCmd.AddCommand(startNodeCmd)
}
