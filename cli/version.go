package cli

import (
	"fmt"
	"github.com/spf13/cobra"
)

// versionCmd is the command to generate operator private/public keys
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "specifies the build version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(cmd.Parent().Version)
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
