package cli

import (
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// generateOperatorKeysCmd is the command to generate operator private/public keys
var generateOperatorKeysCmd = &cobra.Command{
	Use:   "generate-operator-keys",
	Short: "generates ssv operator keys",
	Run: func(cmd *cobra.Command, args []string) {
		logger := zap.L()

		pk, sk, err := rsaencryption.GenerateKeys()
		if err != nil {
			logger.Fatal("Failed to generate operator keys", zap.Error(err))
		}
		logger.Info("generated public key (base64)", fields.PubKey(pk))
		logger.Info("generated private key (base64)", fields.PrivKey(sk))
	},
}

func init() {
	RootCmd.AddCommand(generateOperatorKeysCmd)
}
