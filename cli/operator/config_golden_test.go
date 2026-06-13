package operator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	globalcfg "github.com/ssvlabs/ssv/cli/config"
)

// Test_config_defaults_golden is the project-wide backward-compatibility guard for moving config
// defaults out of cleanenv env-default tags into code. The golden was captured from the original
// tag-based defaults; this asserts ApplyDefaults reproduces them exactly, and that no field silently
// gains or loses a default. It snapshots ApplyDefaults directly (not load) so it stays independent
// of any ambient SSV env vars on the host/CI runner (e.g. HOST_ADDRESS).
//
// Regenerate intentionally — only when a default change is deliberate — with:
//
//	UPDATE_GOLDEN=1 go test ./cli/operator -run Test_config_defaults_golden
func Test_config_defaults_golden(t *testing.T) {
	var c config
	c.ApplyDefaults()

	assertDefaultsGolden(t, filepath.Join("testdata", "defaults.golden.json"), &c)
}

// Test_config_load_missingRequiredFieldErrors guards the other half of the defaults migration:
// ApplyDefaults must NOT seed the env-required fields (eth1 ETH1Addr, eth2 BeaconNodeAddr). Were one
// seeded, it would be non-zero before ReadConfig and cleanenv would silently stop enforcing it. A
// config that omits both must therefore still fail to load.
func Test_config_load_missingRequiredFieldErrors(t *testing.T) {
	var c config
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("p2p:\n  TcpPort: 13001\n"), 0o600))
	require.Error(t, c.load(path, ""))
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
