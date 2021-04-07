package flags

import (
	"github.com/spf13/cobra"

	"github.com/bloxapp/ssv/utils/cliflag"
)

// Flag names.
const (
	bootNodePrivateKeyFlag = "private-key"
	bootNodeExternalIPFlag = "external-ip"
)

// AddBootNodePrivateKeyFlag adds the boot node private key flag to the command
func AddBootNodePrivateKeyFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, bootNodePrivateKeyFlag, "", "boot node private", false)
}

// GetBootNodePrivateKeyFlagValue get the boot node private key flag to the command
func GetBootNodePrivateKeyFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(bootNodePrivateKeyFlag)
}

// GetExternalIPFlagValue gets the external ip flag from the command
func GetExternalIPFlagValue(c *cobra.Command) (string, error) {
	return c.Flags().GetString(bootNodeExternalIPFlag)
}

// AddExternalIPFlag adds the external ip flag to the command
func AddExternalIPFlag(c *cobra.Command) {
	cliflag.AddPersistentStringFlag(c, bootNodeExternalIPFlag, "", "external ip", false)
}
