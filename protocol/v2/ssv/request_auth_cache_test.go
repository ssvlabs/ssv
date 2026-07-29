package ssv

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

func TestRequestAuthCache(t *testing.T) {
	now := phase0.Slot(100)
	cache := NewRequestAuthCache(func() phase0.Slot { return now })
	authAt := func(slot phase0.Slot) *gloas.SignedRequestAuthV1 {
		return &gloas.SignedRequestAuthV1{Message: &gloas.RequestAuthV1{Data: []byte("x"), Slot: slot}}
	}

	require.Empty(t, cache.Get(110))

	cache.Store(110, "builder-a", authAt(110))
	cache.Store(110, "builder-b", authAt(110))
	require.Len(t, cache.Get(110), 2)
	require.Empty(t, cache.Get(111))

	// The returned map is a copy: mutating it must not affect the cache.
	got := cache.Get(110)
	delete(got, "builder-a")
	require.Len(t, cache.Get(110), 2)

	// Eviction is clock-anchored: writing a much later lookahead slot must NOT evict an earlier
	// slot that is still in the future (the §5 lookahead spans two epochs, so a validator can hold
	// proposal slots arbitrarily far apart within it).
	cache.Store(160, "builder-a", authAt(160))
	require.Len(t, cache.Get(110), 2, "a future slot must survive writes for later slots")

	// Once the chain moves past a slot, the next write prunes it; future slots stay.
	now = 111
	cache.Store(160, "builder-b", authAt(160))
	require.Empty(t, cache.Get(110), "a past slot must be evicted")
	require.Len(t, cache.Get(160), 2)
}
