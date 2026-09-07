package operator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/qa/faults"
)

func TestApplyBuilderFault(t *testing.T) {
	t.Run("no fault leaves the config alone", func(t *testing.T) {
		cfg := &gloas.BuilderConfig{}
		require.False(t, applyBuilderFault(cfg))
		require.Empty(t, cfg.Entries)
	})

	t.Run("auth-no-builders injects one entry", func(t *testing.T) {
		faults.SetForTest(t, faults.AuthNoBuilders)
		cfg := &gloas.BuilderConfig{}

		require.True(t, applyBuilderFault(cfg))
		require.Len(t, cfg.Entries, 1)
		require.NotEmpty(t, cfg.Entries[0].URL)
		require.True(t, cfg.Configured())
	})

	t.Run("a real configuration wins", func(t *testing.T) {
		faults.SetForTest(t, faults.AuthNoBuilders)
		cfg := &gloas.BuilderConfig{Entries: []gloas.BuilderEntry{{URL: "http://real.example"}}}

		require.False(t, applyBuilderFault(cfg))
		require.Len(t, cfg.Entries, 1)
		require.Equal(t, "http://real.example", cfg.Entries[0].URL)
	})
}
