package operator

import (
	"os"

	"github.com/aquasecurity/table"
	"github.com/spf13/cobra"

	globalcfg "github.com/ssvlabs/ssv/cli/config"
)

// GenerateDocCmd prints a table documenting every config field with its YAML path, env var, default
// and description. Defaults are read from a config seeded by ApplyDefaults (via cli/config.Describe),
// so the documentation stays in lockstep with the in-code defaults instead of tracking separate
// env-default struct tags.
var GenerateDocCmd = &cobra.Command{
	Use:   "doc",
	Short: "Generate CLI documentation for the node",
	Run: func(cmd *cobra.Command, args []string) {
		var c config
		c.ApplyDefaults()

		tbl := table.New(os.Stdout)
		tbl.SetHeaders("YAML", "ENV", "Default", "Description")
		for _, doc := range globalcfg.Describe(&c) {
			tbl.AddRow(doc.YAMLPath, doc.EnvName, doc.Default, doc.Description)
		}
		tbl.Render()
	},
}
