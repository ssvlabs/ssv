package operator

import (
	"os"
	"reflect"
	"strings"

	"github.com/aquasecurity/table"
	"github.com/spf13/cobra"
)

type ArgumentDoc struct {
	FieldPath   []string
	YAMLPath    []string
	EnvName     string
	Default     string
	Description string
}

var GenerateDocCmd = &cobra.Command{
	Use:   "doc",
	Short: "Generate CLI documentation for the node",
	Run: func(cmd *cobra.Command, args []string) {
		var cfg config
		t := reflect.TypeOf(cfg)

		var docs []ArgumentDoc
		getAllFields(t, nil, nil, &docs)

		tbl := table.New(os.Stdout)
		tbl.SetHeaders("YAML", "ENV", "Default", "Description")
		for _, doc := range docs {
			yamlName := strings.Join(doc.YAMLPath, ".")
			tbl.AddRow(yamlName, doc.EnvName, doc.Default, doc.Description)
		}
		tbl.Render()
	},
}

// Recursive function to get all fields with "env" or "yaml" tags.
func getAllFields(rt reflect.Type, fieldPath, yamlPath []string, docs *[]ArgumentDoc) {
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		yamlName := field.Tag.Get("yaml")
		envName := field.Tag.Get("env")

		if yamlName != "" || envName != "" {
			*docs = append(*docs, ArgumentDoc{
				FieldPath:   append(fieldPath, field.Name),
				YAMLPath:    append(yamlPath, yamlName),
				EnvName:     envName,
				Default:     field.Tag.Get("env-default"),
				Description: field.Tag.Get("env-description"),
			})
		}

		// Recursion for nested structs.
		if yamlName != "" && field.Type.Kind() == reflect.Struct {
			getAllFields(field.Type, append(fieldPath, field.Name), append(yamlPath, yamlName), docs)
		}
	}
}
