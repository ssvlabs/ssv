package faults

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermissiveValueCheckAcceptsAnything(t *testing.T) {
	var c PermissiveValueCheck
	require.NoError(t, c.CheckValue(nil))
	require.NoError(t, c.CheckValue([]byte{}))
	require.NoError(t, c.CheckValue([]byte{0xde, 0xad, 0xbe, 0xef}))
}

// The zero value must be the honest state, so a seam that forgets to set the override keeps the stock
// checker rather than silently weakening it.
func TestValueCheckOverrideZeroValueIsHonest(t *testing.T) {
	var o ValueCheckOverride
	require.Equal(t, KeepHonestCheck, o)
	require.Equal(t, "keep-honest-check", KeepHonestCheck.String())
	require.Equal(t, "drop-same-slot-view", DropSameSlotView.String())
	require.Equal(t, "permissive-check", PermissiveCheck.String())
	require.Equal(t, "unknown", ValueCheckOverride(99).String())
}
