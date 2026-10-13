package goclient

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOptions_ApplyDefaults locks the required-field invariant at the source: ApplyDefaults seeds
// the in-code defaults but must leave the env-required BeaconNodeAddr unset, so cleanenv keeps
// enforcing it.
func TestOptions_ApplyDefaults(t *testing.T) {
	var o Options
	o.ApplyDefaults()

	require.Equal(t, uint64(4), o.SyncDistanceTolerance)
	require.Empty(t, o.BeaconNodeAddr)
}
