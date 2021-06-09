package flags

import (
	"github.com/spf13/cobra"

	"github.com/bloxapp/ssv/utils/cliflag"
)

// Flag names.
const (
	roleFlag = "role"
)

// AddRoleFlag adds the role key flag to the command
func AddRoleFlag(c *cobra.Command) {
	cliflag.AddPersistentIntFlag(c, roleFlag, 100, "role type index", true)
}

// GetRoleFlagValue gets the role key flag from the command
func GetRoleFlagValue(c *cobra.Command) (int, error) {
	return c.Flags().GetInt(roleFlag)
}