package cli

import (
	"encoding/base64"
	"log"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// generateOperatorKeysCmd is the command to generate operator private/public keys
var generateOperatorKeysCmd = &cobra.Command{
	Use:   "generate-operator-keys",
	Short: "generates ssv operator keys",
	Run: func(cmd *cobra.Command, args []string) {
		if err := logging.SetGlobalLogger("debug", "capital", "console"); err != nil {
			log.Fatal(err)
		}
		logger := zap.L().Named(RootCmd.Short)

		pk, sk, err := rsaencryption.GenerateKeys()
		if err != nil {
			logger.Fatal("Failed to generate operator keys", zap.Error(err))
		}
		logger.Info("generated public key (base64)", zap.String("pk", base64.StdEncoding.EncodeToString(pk)))
		logger.Info("generated private key (base64)", zap.String("sk", base64.StdEncoding.EncodeToString(sk)))
	},
}

func init() {
	RootCmd.AddCommand(generateOperatorKeysCmd)
}
