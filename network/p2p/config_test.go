package p2pv1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConfig_ApplyDefaults checks ApplyDefaults seeds the in-code p2p defaults, spot-checking the
// true-default bools that motivated moving defaults out of cleanenv env-default tags (#2868): a
// bool's zero value is false, so env-default:"true" couldn't be told apart from an explicit false.
func TestConfig_ApplyDefaults(t *testing.T) {
	var c Config
	c.ApplyDefaults()
	require.True(t, c.DynamicMaxPeers)
	require.True(t, c.PubSubScoring)
	require.Equal(t, 60, c.MaxPeers)
	require.Equal(t, "discv5", c.Discovery)
}
