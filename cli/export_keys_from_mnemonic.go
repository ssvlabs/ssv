package cli

import (
	"encoding/hex"
	"fmt"
	"log"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/spf13/cobra"
	util "github.com/wealdtech/go-eth2-util"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/cli/flags"
	"github.com/bloxapp/ssv/logging"
)

// exportKeysCmd is the command to export private/public keys based on given mnemonic
var exportKeysCmd = &cobra.Command{
	Use:   "export-keys",
	Short: "exports private/public keys based on given mnemonic",
	Run: func(cmd *cobra.Command, args []string) {
		if err := logging.SetGlobalLogger("dpanic", "capital", "console", nil); err != nil {
			log.Fatal(err)
		}

		logger := zap.L().Named(logging.NameExportKeys)

		mnemonicKey, err := flags.GetMnemonicFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get mnemonic key flag value", zap.Error(err))
		}

		index, err := flags.GetKeyIndexFlagValue(cmd)
		if err != nil {
			logger.Fatal("failed to get key index flag value", zap.Error(err))
		}

		network, err := flags.GetNetworkFlag(cmd)
		if err != nil {
			logger.Fatal("failed to get network flag value", zap.Error(err))
		}

		seed, err := core.SeedFromMnemonic(mnemonicKey, "")
		if err != nil {
			logger.Fatal("failed to get seed from mnemonic", zap.Error(err))
		}

		fmt.Println("Seed:", hex.EncodeToString(seed))
		fmt.Println("Generating keys for index:", index)

		path := core.Network(network).FullPath(fmt.Sprintf("/%d/0/0", index))
		key, err := util.PrivateKeyFromSeedAndPath(seed, path)
		if err != nil {
			logger.Fatal("failed to get private key from seed", zap.Error(err))
		}

		fmt.Println("Private Key:", hex.EncodeToString(key.Marshal()))
		fmt.Println("Public Key:", hex.EncodeToString(key.PublicKey().Marshal()))
	},
}

func init() {
	flags.AddMnemonicFlag(exportKeysCmd)
	flags.AddKeyIndexFlag(exportKeysCmd)
	flags.AddNetworkFlag(exportKeysCmd)

	RootCmd.AddCommand(exportKeysCmd)
}
