package operator

import (
	"testing"

	"github.com/stretchr/testify/require"

	globalcfg "github.com/ssvlabs/ssv/cli/config"
)

// Test_config_defaults_complete makes default-completeness automatic going forward. It fails if any
// env-backed scalar is still at its Go zero value after ApplyDefaults unless it's listed below as
// intentionally having no default. This closes the gap the golden can't: when a field is added (or a
// section's ApplyDefaults isn't wired into the root), the missing default surfaces as an unexpected
// zero and forces a deliberate choice — seed the default, or declare the zero intentional. Unlike the
// golden it needs no regeneration, so it can't be silenced by re-baselining. env-required inputs are
// auto-excluded (cleanenv enforces those).
func Test_config_defaults_complete(t *testing.T) {
	// yamlPath of every env-backed field that legitimately has no non-zero default.
	intentionallyZero := map[string]struct{}{}
	for _, p := range []string{
		// opt-in / disable flags (default off)
		"AllowDangerousProposerDelay", "EnableDoppelgangerProtection", "EnableProfile", "EnableTraces",
		"WithPing", "db.Reporting", "exporter.Enabled", "eth2.WithParallelSubmissions",
		"eth2.WithWeightedAttestationData", "p2p.DisableIPRateLimit", "p2p.DiscoveryTrace",
		"p2p.Libp2pTrace", "p2p.PubSubTrace", "ssv.ValidatorOptions.FullNode",
		// optional values / paths / keys (no default)
		"LocalEventsPath", "NetworkPrivateKey", "OperatorPrivateKey", "SSVAPIAddress",
		"KeyStore.PasswordFile", "KeyStore.PrivateKeyFile", "SSVSigner.Endpoint", "SSVSigner.KeystoreFile",
		"SSVSigner.KeystorePasswordFile", "SSVSigner.ServerCertFile", "p2p.Bootnodes", "p2p.HostAddress",
		"p2p.HostDNS", "p2p.Subnets", "p2p.TrustedPeers", "ssv.CustomDomainType", "ssv.CustomNetwork",
		// optional ports / sizes / timeouts (0 = disabled, or a library/runtime default applies later)
		"MetricsAPIPort", "SSVAPIPort", "WebSocketAPIPort", "ProposerDelay", "ProposerDelayEPBS",
		"eth2.CommonTimeout", "eth2.LongTimeout", "eth2.ProposalSoftTimeout",
		"p2p.PubsubMsgCacheTTL", "p2p.PubsubOutQueueSize", "p2p.PubsubValidateThrottle",
		"p2p.PubsubValidationQueueSize", "ssv.ValidatorOptions.ExperimentalGasLimit",
	} {
		intentionallyZero[p] = struct{}{}
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

	// Keep the list honest: every entry must still exist and still be zero.
	for p := range intentionallyZero {
		z, ok := zeroByPath[p]
		require.Truef(t, ok, "stale intentionallyZero entry %q — field renamed or removed", p)
		require.Truef(t, z, "intentionallyZero entry %q now has a default — remove it from the list", p)
	}
}
