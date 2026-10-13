package bootnode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
//	UPDATE_GOLDEN=1 go test ./cli/bootnode -run Test_config_defaults_golden
func Test_config_defaults_golden(t *testing.T) {
	var c config
	c.ApplyDefaults()

	assertDefaultsGolden(t, filepath.Join("testdata", "defaults.golden.json"), &c)
}

// assertDefaultsGolden snapshots the env-backed scalar fields of a config (via the shared describer)
// and compares them to the committed golden, or rewrites the golden when UPDATE_GOLDEN is set. Kept
// self-contained so cli/operator and cli/bootnode stay independent packages.
func assertDefaultsGolden(t *testing.T, goldenPath string, cfg any) {
	t.Helper()

	snapshot := map[string]string{}
	for _, d := range globalcfg.Describe(cfg) {
		if d.EnvName != "" { // skip nested-struct container rows
			snapshot[d.YAMLPath] = d.Default
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
