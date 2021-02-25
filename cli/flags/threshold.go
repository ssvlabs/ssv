package flags

import (
	"github.com/spf13/cobra"

	"github.com/bloxapp/ssv/utils/cliflag"
)

// Flag names.
const (
	privKeyFlag   = "private-key"
	keysCountFlag = "count"
)

// AddPrivKeyFlag adds the private key flag to the command
func AddPrivKeyFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, privKeyFlag, "", "Hex encoded private key", true)
}

// GetPrivKeyFlagValue gets the private key flag from the command
func GetPrivKeyFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(privKeyFlag)
}

// AddKeysCountFlag adds the keys count flag to the command
func AddKeysCountFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, keysCountFlag, 4, "Count of threshold keys to be generated", false)
}

// GetKeysCountFlagValue gets the keys count flag from the command
func GetKeysCountFlagValue(c *cobra.Command) (uint64, error) {
	return c.Flags().GetUint64(keysCountFlag)
}
