package cli

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/spf13/cobra"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"
	"go.uber.org/zap"
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

		pk, sk, err := rsaencryption.GenerateKeys()
		if err != nil {
			logger.Fatal("Failed to generate keys", zap.Error(err))
		}

		if privateKeyFilePath != "" {
			keyBytes, err := readFile(privateKeyFilePath)
			if err != nil {
				logger.Fatal("Failed to read private key from file", zap.Error(err))
			}
			sk, pk, err = parsePrivateKey(keyBytes)
			if err != nil {
				logger.Fatal("Failed to read private key from file", zap.Error(err))
			}
		}

		if passwordFilePath != "" {
			passwordBytes, err := readFile(passwordFilePath)
			if err != nil {
				logger.Fatal("Failed to read password file", zap.Error(err))
			}
			encryptedJSON, encryptedJSONErr := encryptPrivateKey(sk, pk, passwordBytes)
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
			logger.Info("generated public key (base64)", zap.String("pk", base64.StdEncoding.EncodeToString(pk)))
			logger.Info("generated private key (base64)", zap.String("sk", base64.StdEncoding.EncodeToString(sk)))
		}
	},
}

func parsePrivateKey(keyBytes []byte) ([]byte, []byte, error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(string(keyBytes))
	if err != nil {
		return nil, nil, err
	}
	rsaKey, err := rsaencryption.ConvertPemToPrivateKey(string(decodedBytes))
	if err != nil {
		return nil, nil, err
	}

	skPem := rsaencryption.PrivateKeyToByte(rsaKey)

	operatorPublicKey, err := rsaencryption.ExtractPublicKey(rsaKey)
	if err != nil {
		return nil, nil, err
	}
	pk, err := base64.StdEncoding.DecodeString(operatorPublicKey)
	if err != nil {
		return nil, nil, err
	}
	return skPem, pk, nil
}

func encryptPrivateKey(sk []byte, pk []byte, passwordBytes []byte) ([]byte, error) {
	encryptionPassword := string(passwordBytes)
	encryptedData, err := keystorev4.New().Encrypt(sk, encryptionPassword)
	if err != nil {
		return nil, err
	}
	encryptedData["publicKey"] = base64.StdEncoding.EncodeToString(pk)
	encryptedJSON, err := json.Marshal(encryptedData)
	if err != nil {
		return nil, err
	}
	return encryptedJSON, nil
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
