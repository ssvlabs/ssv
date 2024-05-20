package cli

import (
	"fmt"
	"log"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/spf13/cobra"
	"github.com/ssvlabs/ssv/logging"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/cli/flags"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// createThreshold is the command to create threshold based on the given private key
var createThresholdCmd = &cobra.Command{
	Use:   "create-threshold",
	Short: "Turns a private key into a threshold key",
	Run: func(cmd *cobra.Command, args []string) {
		if err := logging.SetGlobalLogger("debug", "capital", "console", nil); err != nil {
			log.Fatal(err)
		}
		logger := zap.L().Named(logging.NameCreateThreshold)

		privKey, err := flags.GetPrivKeyFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get private key flag value", zap.Error(err))
		}

		keysCount, err := flags.GetKeysCountFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get keys count flag value", zap.Error(err))
		}

		baseKey := &bls.SecretKey{}
		if err := baseKey.SetHexString(privKey); err != nil {
			logger.Fatal("failed to set hex private key", zap.Error(err))
		}

		// https://github.com/ethereum/eth2-ssv/issues/22
		// currently support 4 nodes threshold is keysCount-1(3). need to align based open the issue to
		// support k(2f+1) and n (3f+1) and allow to pass it as flag
		privKeys, err := threshold.Create(baseKey.Serialize(), keysCount-1, keysCount)
		if err != nil {
			logger.Fatal("failed to turn a private key into a threshold key", zap.Error(err))
		}

		// TODO: export to json file
		fmt.Println("Generating threshold keys for validator", baseKey.GetPublicKey().SerializeToHexStr())
		for i, pk := range privKeys {
			fmt.Println()
			fmt.Println("Public key", i, pk.GetPublicKey().SerializeToHexStr())
			fmt.Println("Private key", i, pk.SerializeToHexStr())
		}
	},
}

func init() {
	flags.AddPrivKeyFlag(createThresholdCmd)
	flags.AddKeysCountFlag(createThresholdCmd)

	RootCmd.AddCommand(createThresholdCmd)
}
