package bootnode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	globalcfg "github.com/ssvlabs/ssv/cli/config"
)

// Test_config_defaults_golden is the bootnode half of the defaults backward-compatibility guard.
// The golden was captured from the original tag-based defaults; this asserts ApplyDefaults
// reproduces them exactly. It snapshots ApplyDefaults directly (not Prepare) so it stays independent
// of any ambient SSV env vars on the host/CI runner.
//
// Regenerate intentionally with:
//
//	UPDATE_GOLDEN=1 go test ./cli/bootnode -run _golden
func Test_config_defaults_golden(t *testing.T) {
	var c config
	c.ApplyDefaults()

	assertDescribeGolden(t, filepath.Join("testdata", "defaults.golden.json"), &c,
		func(d globalcfg.FieldDoc) string { return d.Default })
}

// Test_config_envNames_golden pins every env var name. The names otherwise live only in struct tags,
// so a typo or rename there would silently stop an operator's env var from applying, and a test that
// reads the name back from the tag would read the typo too. With the names in a reviewed file, any
// change shows up as a golden diff.
//
// It guards existing names only: a typo in a new field's tag goes into the golden with the regen, so
// the golden diff is where it has to be caught.
//
// Regenerate intentionally, only when an env var is deliberately added or renamed, with:
//
//	UPDATE_GOLDEN=1 go test ./cli/bootnode -run _golden
func Test_config_envNames_golden(t *testing.T) {
	var c config
	c.ApplyDefaults()

	assertDescribeGolden(t, filepath.Join("testdata", "envnames.golden.json"), &c,
		func(d globalcfg.FieldDoc) string { return d.EnvName })
}

// Test_config_envNames_matchCleanenv checks that the names the goldens pin are the names cleanenv
// reads. Describe works the names out on its own, and it differs from cleanenv in two ways no field
// hits today: it only recurses into nested structs that carry a yaml tag, and it treats an
// `env:"A,B"` alias list as one name.
func Test_config_envNames_matchCleanenv(t *testing.T) {
	var c config
	c.ApplyDefaults()

	var described []string
	for _, d := range globalcfg.Describe(&c) {
		if d.EnvName != "" {
			described = append(described, d.EnvName)
		}
	}
	assert.ElementsMatch(t, cleanenvEnvNames(t, &c), described)
}

// Test_config_fields_wellFormed checks two invariants the goldens don't: no two fields share an env
// var (cleanenv would set both from one variable), and every env-backed field has an
// env-description (it is what --help and the generated config docs print).
func Test_config_fields_wellFormed(t *testing.T) {
	var c config
	c.ApplyDefaults()

	fieldByEnv := map[string]string{}
	for _, d := range globalcfg.Describe(&c) {
		if d.EnvName == "" {
			continue // nested-struct container, not a field
		}
		if other, dup := fieldByEnv[d.EnvName]; dup {
			t.Errorf("env var %s is shared by %s and %s", d.EnvName, other, d.YAMLPath)
		}
		fieldByEnv[d.EnvName] = d.YAMLPath
		assert.NotEmptyf(t, d.Description, "%s has no env-description", d.YAMLPath)
	}
}

// assertDescribeGolden snapshots one column of the env-backed scalar fields of a config (via the
// shared describer), keyed by YAML path, and compares it to the committed golden, or rewrites the
// golden when UPDATE_GOLDEN is set. Kept self-contained so cli/operator and cli/bootnode stay
// independent packages.
func assertDescribeGolden(t *testing.T, goldenPath string, cfg any, column func(globalcfg.FieldDoc) string) {
	t.Helper()

	snapshot := map[string]string{}
	for _, d := range globalcfg.Describe(cfg) {
		if d.EnvName != "" { // skip nested-struct container rows
			snapshot[d.YAMLPath] = column(d)
		}
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	require.NoError(t, err)
	data = append(data, '\n')

	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
		require.NoError(t, os.WriteFile(goldenPath, data, 0o644))
		t.Logf("wrote golden %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "missing golden file; regenerate with UPDATE_GOLDEN=1")
	require.JSONEq(t, string(want), string(data))
}

// cleanenvEnvNames lists the env vars cleanenv reads for cfg. cleanenv doesn't export its field
// metadata, so the names are parsed out of its help text, where each variable is a two-space
// indented "NAME kind" line.
func cleanenvEnvNames(t *testing.T, cfg any) []string {
	t.Helper()

	header := ""
	help, err := cleanenv.GetDescription(cfg, &header)
	require.NoError(t, err)

	var names []string
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			names = append(names, strings.Fields(line)[0])
		}
	}
	require.NotEmpty(t, names)
	return names
}
