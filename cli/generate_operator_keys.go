package cli

import (
	"log"
	"os"
	"path/filepath"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keystore"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
)

var generateOperatorKeysCmd = &cobra.Command{
	Use:   "generate-operator-keys",
	Short: "generates ssv operator keys",
	Run: func(cmd *cobra.Command, args []string) {
		if err := logging.SetGlobalLogger("debug", "capital", "console", nil); err != nil {
			log.Fatal(err)
		}

		logger := zap.L().Named(logging.NameExportKeys)
		passwordFilePath, _ := cmd.Flags().GetString("password-file")
		privateKeyFilePath, _ := cmd.Flags().GetString("operator-key-file")

		privKey, err := keys.GeneratePrivateKey()
		if err != nil {
			logger.Fatal("Failed to generate keys", zap.Error(err))
		}

		if privateKeyFilePath != "" {
			keyBytes, err := readFile(privateKeyFilePath)
			if err != nil {
				logger.Fatal("Failed to read private key from file", zap.Error(err))
			}

			privKey, err = keys.PrivateKeyFromString(string(keyBytes))
			if err != nil {
				logger.Fatal("Failed to parse private key", zap.Error(err))
			}
		}

		pubKeyBase64, err := privKey.Public().Base64()
		if err != nil {
			logger.Fatal("Failed to get public key PEM", zap.Error(err))
		}

		if passwordFilePath != "" {
			passwordBytes, err := readFile(passwordFilePath)
			if err != nil {
				logger.Fatal("Failed to read password file", zap.Error(err))
			}

			encryptedJSON, encryptedJSONErr := keystore.EncryptKeystore(privKey.Bytes(), pubKeyBase64, string(passwordBytes))
			if encryptedJSONErr != nil {
				logger.Fatal("Failed to encrypt private key", zap.Error(err))
			}

			err = writeFile("encrypted_private_key.json", encryptedJSON)
			if err != nil {
				logger.Fatal("Failed to save private key", zap.Error(err))
			} else {
				logger.Info("private key encrypted and stored in encrypted_private_key.json")
			}
		} else {
			logger.Info("generated public key (base64)", zap.String("pk", pubKeyBase64))
			logger.Info("generated private key (base64)", zap.String("sk", privKey.Base64()))
		}
	},
}

func writeFile(fileName string, data []byte) error {
	return os.WriteFile(fileName, data, 0600)
}

func readFile(filePath string) ([]byte, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	// #nosec G304
	contentBytes, err := os.ReadFile(absPath)
	return contentBytes, err
}

func init() {
	generateOperatorKeysCmd.Flags().StringP("password-file", "p", "", "File path to the password used to encrypt the private key")
	generateOperatorKeysCmd.Flags().StringP("operator-key-file", "o", "", "File path to the operator private key")
	RootCmd.AddCommand(generateOperatorKeysCmd)
}
