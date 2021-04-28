package cli

import (
	"crypto/ecdsa"
	"fmt"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// generateOperatorKeysCmd is the command to generate operator private/public keys
var generateOperatorKeysCmd = &cobra.Command{
	Use:   "generate-operator-keys",
	Short: "generates ssv operator keys",
	Run: func(cmd *cobra.Command, args []string) {
		// generate a random private key
		privateKey, err := crypto.GenerateKey()
		if err != nil {
			Logger.Fatal("failed to generate private key", zap.Error(err))
		}

		// convert to bytes
		privateKeyBytes := crypto.FromECDSA(privateKey)
		publicKey := privateKey.Public()

		publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
		if !ok {
			Logger.Fatal("failed to cast public key to ECDSA", zap.Error(err))
		}

		publicKeyBytes := crypto.FromECDSAPub(publicKeyECDSA)

		fmt.Println("Private Key:", hexutil.Encode(privateKeyBytes)[2:])
		fmt.Println("Public Key:", hexutil.Encode(publicKeyBytes)[4:])
	},
}

func init() {
	RootCmd.AddCommand(generateOperatorKeysCmd)
}
