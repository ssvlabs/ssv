package cli

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// runGenerateConfig executes the generate-config command against a temp output
// path and returns the parsed result. The command reads package-level flag
// vars whose defaults are installed by init(); callers mutate specific vars
// before invoking and this helper restores everything it touches.
func runGenerateConfig(t *testing.T) SSVConfig {
	t.Helper()

	out := filepath.Join(t.TempDir(), "config.yaml")
	origOutput, origConsensus, origExecution := outputPath, consensusClient, executionClient
	t.Cleanup(func() {
		outputPath, consensusClient, executionClient = origOutput, origConsensus, origExecution
	})
	outputPath, consensusClient, executionClient = out, "http://localhost:5052", "ws://localhost:8546"

	generateConfigCmd.Run(generateConfigCmd, nil)

	data, err := os.ReadFile(out)
	require.NoError(t, err)

	var cfg SSVConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	require.NotNil(t, cfg.SSV.CustomNetwork)
	return cfg
}

func TestGenerateConfigForkDefaults(t *testing.T) {
	cfg := runGenerateConfig(t)

	// Without flags the generated config must describe an unscheduled fork:
	// Boole pinned to MaxUint64 and the default network's real next domain,
	// never the zero values (a fork-at-genesis config with a zero domain).
	require.Equal(t, phase0.Epoch(math.MaxUint64), cfg.SSV.CustomNetwork.Forks.Boole)
	require.Equal(t, defaultNetwork.NextDomainType, cfg.SSV.CustomNetwork.NextDomainType)
	require.Equal(t, defaultNetwork.DomainType, cfg.SSV.CustomNetwork.DomainType)
}

func TestGenerateConfigForkOverrides(t *testing.T) {
	origEpoch, origNextDomain := ssvBooleForkEpoch, ssvNextDomain
	t.Cleanup(func() {
		ssvBooleForkEpoch, ssvNextDomain = origEpoch, origNextDomain
	})
	ssvBooleForkEpoch = 100
	ssvNextDomain = "0x00000404"

	cfg := runGenerateConfig(t)

	require.Equal(t, phase0.Epoch(100), cfg.SSV.CustomNetwork.Forks.Boole)
	require.Equal(t, spectypes.DomainType{0x00, 0x00, 0x04, 0x04}, cfg.SSV.CustomNetwork.NextDomainType)
}
