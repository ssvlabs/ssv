package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRelays(t *testing.T) {
	t.Parallel()

	got := NormalizeRelays([]string{"https://relay1;https://relay2", " https://relay2 ", "https://relay3,https://relay4", ""})
	require.Equal(t, []string{"https://relay1", "https://relay2", "https://relay3", "https://relay4"}, got)

	require.Nil(t, NormalizeRelays(nil))
	require.Nil(t, NormalizeRelays([]string{""}))
}
