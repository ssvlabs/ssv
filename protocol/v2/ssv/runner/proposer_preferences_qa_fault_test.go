package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/qa/faults"
)

func TestApplyPreferenceFault(t *testing.T) {
	honest := bellatrix.ExecutionAddress{0x11, 0x22, 0x33}

	t.Run("no fault", func(t *testing.T) {
		require.Equal(t, honest, applyPreferenceFault(honest, 100))
	})

	t.Run("prefs-conflict diverges from the cluster", func(t *testing.T) {
		faults.SetForTest(t, faults.PrefsConflict)
		got := applyPreferenceFault(honest, 100)
		require.NotEqual(t, honest, got)
		// Stable across slots: the divergence is a standing disagreement, not a moving target.
		require.Equal(t, got, applyPreferenceFault(honest, 220))
	})

	t.Run("prefs-34-apart gives slots 34 apart different roots", func(t *testing.T) {
		faults.SetForTest(t, faults.Prefs34Apart)
		a := applyPreferenceFault(honest, phase0.Slot(100))
		b := applyPreferenceFault(honest, phase0.Slot(134))
		require.NotEqual(t, a, b)
		// Deterministic per slot, so a re-emission for the same slot keeps its root.
		require.Equal(t, a, applyPreferenceFault(honest, phase0.Slot(100)))
	})
}
