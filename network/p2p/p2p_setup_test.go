package p2pv1

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBackoffRandSourceUsesConfiguredSeed(t *testing.T) {
	originalSeedFunc := backoffSeedFunc
	backoffSeedFunc = func() int64 { return 12345 }
	t.Cleanup(func() {
		backoffSeedFunc = originalSeedFunc
	})

	got := rand.New(newBackoffRandSource()).Int63()
	want := rand.New(rand.NewSource(12345)).Int63()

	require.Equal(t, want, got)
	require.NotEqual(t, rand.New(rand.NewSource(0)).Int63(), got)
}
