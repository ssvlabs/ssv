package operator

import (
	"os"
	"reflect"

	"github.com/aquasecurity/table"
	"github.com/spf13/cobra"
)

type ArgumentDoc struct {
	FieldName   string
	YAMLName    string
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
		getAllFields(t, "", "", &docs)

		tbl := table.New(os.Stdout)
		tbl.SetHeaders("YAML", "ENV", "Default", "Description")
		for _, doc := range docs {
			tbl.AddRow(doc.YAMLName, doc.EnvName, doc.Default, doc.Description)
		}
		tbl.Render()
	},
}

// Recursive function to get all fields with "env" or "yaml" tags.
func getAllFields(rt reflect.Type, prefix string, yamlPrefix string, docs *[]ArgumentDoc) {
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		// Generate the full path of field names for nested fields.
		fieldName := prefix + field.Name
		yamlName := yamlPrefix + field.Tag.Get("yaml")
		envTag := field.Tag.Get("env")

		if yamlName != yamlPrefix || envTag != "" {
			*docs = append(*docs, ArgumentDoc{
				FieldName:   fieldName,
				YAMLName:    yamlName,
				EnvName:     envTag,
				Default:     field.Tag.Get("env-default"),
				Description: field.Tag.Get("env-description"),
			})
		}

		// Recursion for nested structs.
		if field.Type.Kind() == reflect.Struct {
			getAllFields(field.Type, fieldName+".", yamlName+".", docs)
		}
	}
}
