package bootnode

import (
	"testing"

	"github.com/stretchr/testify/require"

	globalcfg "github.com/ssvlabs/ssv/cli/config"
)

// Test_config_defaults_complete fails if any env-backed scalar is still at its Go zero value after
// ApplyDefaults unless it's listed as intentionally having no default. See the operator equivalent
// for the rationale; this guards the bootnode tree the same way, without needing golden regeneration.
func Test_config_defaults_complete(t *testing.T) {
	intentionallyZero := map[string]struct{}{
		"bootnode.ExternalIP": {}, // optional external IP override
		"bootnode.PrivateKey": {}, // optional; generated if empty
	}

	var c config
	c.ApplyDefaults()
	assertDefaultsComplete(t, &c, intentionallyZero)
}

// assertDefaultsComplete checks that every env-backed scalar in cfg is non-zero after ApplyDefaults
// unless its YAML path is in intentionallyZero, and that intentionallyZero has no stale/over-claiming
// entries. Kept self-contained so cli/operator and cli/bootnode stay independent packages.
func assertDefaultsComplete(t *testing.T, cfg any, intentionallyZero map[string]struct{}) {
	t.Helper()

	zeroRendering := map[string]bool{"": true, "0": true, "false": true, "0s": true}
	zeroByPath := map[string]bool{}
	for _, d := range globalcfg.Describe(cfg) {
		if d.EnvName == "" {
			continue // nested-struct container, not a field
		}
		isZero := !d.Required && zeroRendering[d.Default]
		zeroByPath[d.YAMLPath] = isZero
		if isZero {
			_, ok := intentionallyZero[d.YAMLPath]
			require.Truef(t, ok,
				"%s (%s) is zero after ApplyDefaults and not declared intentional — seed its default in the "+
					"relevant ApplyDefaults, or add it to intentionallyZero with a rationale", d.YAMLPath, d.EnvName)
		}
	}

	for p := range intentionallyZero {
		z, ok := zeroByPath[p]
		require.Truef(t, ok, "stale intentionallyZero entry %q — field renamed or removed", p)
		require.Truef(t, z, "intentionallyZero entry %q now has a default — remove it from the list", p)
	}
}
