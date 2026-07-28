package ssv

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

func TestRequestAuthCache(t *testing.T) {
	cache := NewRequestAuthCache()
	authAt := func(slot phase0.Slot) *gloas.SignedRequestAuthV1 {
		return &gloas.SignedRequestAuthV1{Message: &gloas.RequestAuthV1{Data: []byte("x"), Slot: slot}}
	}

	require.Empty(t, cache.Get(1, 100))

	cache.Store(1, 100, "builder-a", authAt(100))
	cache.Store(1, 100, "builder-b", authAt(100))
	cache.Store(2, 100, "builder-a", authAt(100))

	require.Len(t, cache.Get(1, 100), 2)
	require.Len(t, cache.Get(2, 100), 1)
	require.Empty(t, cache.Get(3, 100))

	// The returned map is a copy: mutating it must not affect the cache.
	got := cache.Get(1, 100)
	delete(got, "builder-a")
	require.Len(t, cache.Get(1, 100), 2)

	// Slots more than the retention window behind the newest stored slot are evicted per validator.
	cache.Store(1, 100+requestAuthRetentionSlots+1, "builder-a", authAt(100+requestAuthRetentionSlots+1))
	require.Empty(t, cache.Get(1, 100), "stale slot must be evicted")
	require.Len(t, cache.Get(2, 100), 1, "other validators' slots are untouched")
}
