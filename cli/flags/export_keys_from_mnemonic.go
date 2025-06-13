package flags

import (
	"github.com/spf13/cobra"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils/cliflag"
)

// Flag names.
const (
	mnemonicFlag = "mnemonic"
	indexFlag    = "index"
	networkFlag  = "network"
)

// AddMnemonicFlag adds the mnemonic key flag to the command
func AddMnemonicFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, mnemonicFlag, "", "24 letter mnemonic phrase", true)
}

// GetMnemonicFlagValue gets the mnemonic key flag from the command
func GetMnemonicFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(mnemonicFlag)
}

// AddKeyIndexFlag adds the key index flag to the command
func AddKeyIndexFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, indexFlag, 0, "Index of the key to export from mnemonic", false)
}

// GetKeyIndexFlagValue gets the key index flag to the command
func GetKeyIndexFlagValue(c *cobra.Command) (uint64, error) {
	return c.Flags().GetUint64(indexFlag)
}

// AddNetworkFlag adds the network key flag to the command
func AddNetworkFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, networkFlag, networkconfig.MainnetName, "network", false)
}

// GetNetworkFlag gets the network key flag from the command
func GetNetworkFlag(c *cobra.Command) (string, error) {
	return c.Flags().GetString(networkFlag)
}
