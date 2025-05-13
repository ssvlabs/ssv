package cliflag

import (
	"fmt"

	"github.com/spf13/cobra"
)

// AddPersistentStringFlag adds a string flag to the command
func AddPersistentStringFlag(c *cobra.Command, flag string, value string, description string, isRequired bool) {
	c.PersistentFlags().String(flag, value, formatDescription(description, isRequired))
	markRequired(c, flag, isRequired)
}

// AddPersistentIntFlag adds a int flag to the command
func AddPersistentIntFlag(c *cobra.Command, flag string, value uint64, description string, isRequired bool) {
	c.PersistentFlags().Uint64(flag, value, formatDescription(description, isRequired))
	markRequired(c, flag, isRequired)
}

// formatDescription adds required suffix to description if needed
func formatDescription(description string, isRequired bool) string {
	const requiredSuffix = " (required)"
	if isRequired {
		return fmt.Sprintf("%s%s", description, requiredSuffix)
	}
	return description
}

// markRequired marks flag as required if needed, ignoring errors
func markRequired(c *cobra.Command, flag string, isRequired bool) {
	if isRequired {
		_ = c.MarkPersistentFlagRequired(flag)
	}
}
