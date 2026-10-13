package executionclient

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOptions_ApplyDefaults locks the required-field invariant at the source: ApplyDefaults seeds
// the in-code defaults but must leave the env-required Addr (ETH1Addr) unset, so cleanenv keeps
// enforcing it.
func TestOptions_ApplyDefaults(t *testing.T) {
	var o Options
	o.ApplyDefaults()

	require.Equal(t, 10*time.Second, o.ConnectionTimeout)
	require.Equal(t, uint64(5), o.SyncDistanceTolerance)
	require.Empty(t, o.Addr)
}
