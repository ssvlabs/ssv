package cli

import (
	"github.com/bloxapp/ssv/logging"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// generateOperatorKeysCmd is the command to generate operator private/public keys
var generateOperatorKeysCmd = &cobra.Command{
	Use:   "generate-operator-keys",
	Short: "generates ssv operator keys",
	Run: func(cmd *cobra.Command, args []string) {
		logger := logging.Build(RootCmd.Short, zapcore.DebugLevel, nil)

		pk, sk, err := rsaencryption.GenerateKeys()
		if err != nil {
			logger.Fatal("Failed to generate operator keys", zap.Error(err))
		}
		logger.Info("generated public key (base64)", logging.PubKey(pk))
		logger.Info("generated private key (base64)", logging.PrivKey(sk))
	},
}

func init() {
	RootCmd.AddCommand(generateOperatorKeysCmd)
}
