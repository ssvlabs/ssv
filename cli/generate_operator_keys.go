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
		if err := logging.SetGlobalLogger("debug", "capital", "console", ""); err != nil {
			log.Fatal(err)
		}
		logger := zap.L().Named(RootCmd.Short)

		pk, sk, err := rsaencryption.GenerateKeys()
		if err != nil {
			logger.Fatal("Failed to generate operator keys", zap.Error(err))
		}
		logger.Info("generated public key (base64)", zap.String("pk", base64.StdEncoding.EncodeToString(pk)))
		logger.Info("generated private key (base64)", zap.String("sk", base64.StdEncoding.EncodeToString(sk)))
		logger.Info("This command generates RSA public and private keys in base64 format.\n\nFor enhanced security, it is recommended to store the private key in an encrypted PEM format. This provides an additional layer of protection by requiring a passphrase to be used whenever the private key is accessed. \n\nYou can create an encrypted RSA private key in PEM format using the following command:\n\n$ ssh-keygen -t rsa -b 2048 -f <your private key file path> -N <your_password>\n\nReplace 'your_password' with a secure passphrase of your choice. This passphrase will be required whenever the private key is accessed.\n\nPlease note: The passphrase should be kept securely, and should not be forgotten. Loss of the passphrase will result in inability to access the private key.")
	},
}

func init() {
	RootCmd.AddCommand(generateOperatorKeysCmd)
}
