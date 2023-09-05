package cli

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"

	"os"
	"path/filepath"

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
		logger := zap.L().Named(RootCmd.Short)
		passwordFilePath, _ := cmd.Flags().GetString("password-file")
		privateKeyFilePath, _ := cmd.Flags().GetString("operator-key-file")
		pk, sk, err := rsaencryption.GenerateKeys()
		if err != nil && privateKeyFilePath == "" {
			logger.Fatal("Failed to create key and operator key wasn't provided", zap.Error(err))
		}

		// Resolve to absolute path
		passwordAbsPath, err := filepath.Abs(passwordFilePath)
		if err != nil {
			logger.Fatal("Failed to read absolute path of password file", zap.Error(err))
		}

		// Now read the file
		// #nosec G304
		passwordBytes, err := os.ReadFile(passwordAbsPath)
		if err != nil {
			logger.Fatal("Failed to read password file", zap.Error(err))
		}

		encryptionPassword := string(passwordBytes)

		if privateKeyFilePath != "" {
			// Resolve to absolute path
			privateKeyAbsPath, err := filepath.Abs(privateKeyFilePath)
			if err != nil {
				logger.Fatal("Failed to read absolute path of private key file", zap.Error(err))
			}

			// Now read the file
			// #nosec G304
			privateKeyBytes, _ := os.ReadFile(privateKeyAbsPath)
			if privateKeyBytes != nil {
				keyBytes, err := base64.StdEncoding.DecodeString(string(privateKeyBytes))
				if err != nil {
					logger.Fatal("base64 decoding failed", zap.Error(err))
				}

				keyPem, _ := pem.Decode(keyBytes)
				if keyPem == nil {
					logger.Fatal("failed to decode PEM", zap.Error(err))
				}

				rsaKey, err := x509.ParsePKCS1PrivateKey(keyPem.Bytes)
				if err != nil {
					logger.Fatal("failed to parse RSA private key", zap.Error(err))
				}

				skPem := pem.EncodeToMemory(
					&pem.Block{
						Type:  "RSA PRIVATE KEY",
						Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
					},
				)

				operatorPublicKey, _ := rsaencryption.ExtractPublicKey(rsaKey)
				publicKey, _ := base64.StdEncoding.DecodeString(operatorPublicKey)
				sk = skPem
				pk = publicKey
			}
		}

		if err := logging.SetGlobalLogger("debug", "capital", "console", ""); err != nil {
			logger.Fatal("", zap.Error(err))
		}

		if err != nil {
			logger.Fatal("Failed to generate operator keys", zap.Error(err))
		}
		logger.Info("generated public key (base64)", zap.String("pk", base64.StdEncoding.EncodeToString(pk)))

		if encryptionPassword != "" {
			encryptedData, err := keystorev4.New().Encrypt(sk, encryptionPassword)
			if err != nil {
				logger.Fatal("Failed to encrypt private key", zap.Error(err))
			}

			encryptedJSON, err := json.Marshal(encryptedData)
			if err != nil {
				logger.Fatal("Failed to marshal encrypted data to JSON", zap.Error(err))
			}

			err = os.WriteFile("encrypted_private_key.json", encryptedJSON, 0600)
			if err != nil {
				logger.Fatal("Failed to write encrypted private key to file", zap.Error(err))
			}

			logger.Info("private key encrypted and stored in encrypted_private_key.json")
		} else {
			logger.Info("generated public key (base64)", zap.String("pk", base64.StdEncoding.EncodeToString(pk)))
			logger.Info("generated private key (base64)", zap.String("sk", base64.StdEncoding.EncodeToString(sk)))
		}
	},
}

func init() {
	generateOperatorKeysCmd.Flags().StringP("password-file", "p", "", "File path to the password used to encrypt the private key")
	generateOperatorKeysCmd.Flags().StringP("operator-key-file", "o", "", "File path to the operator private key")
	RootCmd.AddCommand(generateOperatorKeysCmd)
}
