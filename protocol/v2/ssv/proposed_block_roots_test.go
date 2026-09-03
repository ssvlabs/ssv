package ssv

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestProposedBlockRoots(t *testing.T) {
	s := NewProposedBlockRoots()

	_, ok := s.Get(5)
	require.False(t, ok)

	s.Set(5, phase0.Root{0x01})
	root, ok := s.Get(5)
	require.True(t, ok)
	require.Equal(t, phase0.Root{0x01}, root)

	// A far-future slot evicts roots beyond the retention window.
	s.Set(20, phase0.Root{0x02})
	_, ok = s.Get(5)
	require.False(t, ok, "slot 5 should be evicted beyond the retention window")
	root, ok = s.Get(20)
	require.True(t, ok)
	require.Equal(t, phase0.Root{0x02}, root)
}
