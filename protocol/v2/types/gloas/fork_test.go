package gloas

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/stretchr/testify/require"
)

func TestDataVersionGloas_Placeholder(t *testing.T) {
	// Gloas slots immediately after the current upstream max (Fulu = 7). If this fails,
	// go-eth2-client's DataVersion enum shifted — reconcile the placeholder.
	require.Equal(t, spec.DataVersion(8), DataVersionGloas)

	require.True(t, IsGloas(DataVersionGloas))
	require.False(t, IsGloas(spec.DataVersionFulu))
	require.False(t, IsGloas(spec.DataVersionElectra))
}
