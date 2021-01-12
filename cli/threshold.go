package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/cli/flags"
	"github.com/bloxapp/ssv/utils/threshold"
)

// createThreshold is the command to create threshold based on the given private key
var createThresholdCmd = &cobra.Command{
	Use:   "create-threshold",
	Short: "Turns a private key into a threshold key",
	Run: func(cmd *cobra.Command, args []string) {
		privKey, err := flags.GetPrivKeyFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get private key flag value", zap.Error(err))
		}

		keysCount, err := flags.GetKeysCountFlagValue(cmd)
		if err != nil {
			Logger.Fatal("failed to get keys count flag value", zap.Error(err))
		}

		baseKey, privKeys, err := threshold.Create(privKey, keysCount)
		if err != nil {
			Logger.Fatal("failed to turn a private key into a threshold key", zap.Error(err))
		}

		fmt.Println("Generating threshold keys for validator", baseKey.GetPublicKey().SerializeToHexStr())
		for i, pk := range privKeys {
			fmt.Println()
			fmt.Println("Public key", i+1, pk.GetPublicKey().SerializeToHexStr())
			fmt.Println("Private key", i+1, pk.SerializeToHexStr())
		}
	},
}

func init() {
	flags.AddPrivKeyFlag(createThresholdCmd)
	flags.AddKeysCountFlag(createThresholdCmd)

	RootCmd.AddCommand(createThresholdCmd)
}
