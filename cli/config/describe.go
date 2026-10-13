package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// FieldDoc describes a single configurable field for CLI help / documentation output.
type FieldDoc struct {
	YAMLPath    string // dotted YAML path, e.g. "p2p.MaxPeers"; empty for the root
	EnvName     string // full env var name incl. any inherited env-prefix; empty for nested-struct containers
	Default     string // the field's current value, formatted for display
	Description string // env-description tag
	Required    bool   // env-required:"true"
}

// Describe walks cfg (a struct, or pointer to one) and returns a FieldDoc for every field carrying
// a `yaml` or `env` tag, recursing into nested yaml structs. The Default is taken from each field's
// current value, so callers pass an instance with defaults already applied (see Defaulter) to
// document the defaults.
func Describe(cfg any) []FieldDoc {
	v := reflect.ValueOf(cfg)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	var docs []FieldDoc
	describeStruct(v, nil, "", &docs)
	return docs
}

func describeStruct(rv reflect.Value, yamlPath []string, envPrefix string, docs *[]FieldDoc) {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		yamlName := strings.Split(field.Tag.Get("yaml"), ",")[0] // drop yaml options like ",omitempty"
		envName := field.Tag.Get("env")
		if yamlName == "" && envName == "" {
			continue
		}

		path := yamlPath
		if yamlName != "" {
			// Copy rather than append-in-place so sibling fields and recursion don't share
			// (and corrupt) the same backing array.
			path = append(append([]string{}, yamlPath...), yamlName)
		}

		if envName != "" {
			envName = envPrefix + envName // mirror cleanenv: an ancestor env-prefix applies to nested env vars
		}

		*docs = append(*docs, FieldDoc{
			YAMLPath:    strings.Join(path, "."),
			EnvName:     envName,
			Default:     formatValue(rv.Field(i)),
			Description: field.Tag.Get("env-description"),
			Required:    field.Tag.Get("env-required") == "true",
		})

		if yamlName != "" && field.Type.Kind() == reflect.Struct {
			describeStruct(rv.Field(i), path, envPrefix+field.Tag.Get("env-prefix"), docs)
		}
	}
}

// formatValue renders a scalar field value the way it should appear as a config default. Non-scalar
// kinds (structs, pointers, interfaces, ...) render empty: struct containers are recursed into
// separately, and injected runtime dependencies aren't operator-configurable.
func formatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Type() == reflect.TypeOf(time.Duration(0)) {
			return time.Duration(v.Int()).String()
		}
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts[i] = formatValue(v.Index(i))
		}
		return strings.Join(parts, ";")
	default:
		return ""
	}
}

// describeHelp renders the env-var help block for cfg — each variable with its in-code default and
// description — replacing cleanenv.GetDescription. A fresh defaulted copy is described so the
// caller's live config is left untouched.
func describeHelp(cfg any) string {
	t := reflect.TypeOf(cfg)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	defaulted := reflect.New(t)
	if d, ok := defaulted.Interface().(Defaulter); ok {
		d.ApplyDefaults()
	}

	var b strings.Builder
	b.WriteString("Environment variables:\n")
	for _, doc := range Describe(defaulted.Interface()) {
		if doc.EnvName == "" {
			continue // nested-struct container, no env var of its own
		}
		fmt.Fprintf(&b, "  %s", doc.EnvName)
		switch {
		case doc.Required:
			b.WriteString(" (required)")
		case doc.Default != "":
			fmt.Fprintf(&b, " (default %q)", doc.Default)
		}
		b.WriteString("\n")
		if doc.Description != "" {
			fmt.Fprintf(&b, "    %s\n", doc.Description)
		}
	}
	return b.String()
}
