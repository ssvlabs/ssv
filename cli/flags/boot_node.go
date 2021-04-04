package flags

import (
	"github.com/spf13/cobra"

	"github.com/bloxapp/ssv/utils/cliflag"
)

// Flag names.
const (
	bootNodePrivateKeyFlag = "private-key"
)

// AddBootNodePrivateKeyFlag adds the boot node private key flag to the command
func AddBootNodePrivateKeyFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, bootNodePrivateKeyFlag, "", "boot node private", false)
}

// GetBootNodePrivateKeyFlagValue get the boot node private key flag to the command
func GetBootNodePrivateKeyFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(bootNodePrivateKeyFlag)
}
