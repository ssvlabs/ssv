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
