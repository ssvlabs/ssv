package config

import (
	"testing"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/stretchr/testify/require"
)

func TestProbePeerVersionsDefaultsOff(t *testing.T) {
	var o Options
	o.ApplyDefaults()
	require.False(t, o.ProbePeerVersions)
}

func TestProbePeerVersionsFromEnv(t *testing.T) {
	t.Setenv("EXPORTER_PROBE_PEER_VERSIONS", "true")
	var o Options
	require.NoError(t, cleanenv.ReadEnv(&o))
	require.True(t, o.ProbePeerVersions)
}
