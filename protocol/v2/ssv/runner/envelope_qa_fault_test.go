package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/qa/faults"
)

func TestApplyEnvelopeFault(t *testing.T) {
	newEnvelope := func() *gloas.BlindedExecutionPayloadEnvelope {
		return &gloas.BlindedExecutionPayloadEnvelope{
			PayloadRoot:     phase0.Root{0x01},
			BuilderIndex:    gloas.BuilderIndex(42),
			BeaconBlockRoot: phase0.Root{0x02},
		}
	}

	t.Run("no fault", func(t *testing.T) {
		e := newEnvelope()
		require.False(t, applyEnvelopeFault(e))
		require.Equal(t, phase0.Root{0x02}, e.BeaconBlockRoot)
		require.Equal(t, gloas.BuilderIndex(42), e.BuilderIndex)
	})

	t.Run("envelope-foreign-root", func(t *testing.T) {
		faults.SetForTest(t, faults.EnvelopeForeignRoot)
		e := newEnvelope()
		require.True(t, applyEnvelopeFault(e))
		require.NotEqual(t, phase0.Root{0x02}, e.BeaconBlockRoot)
		require.Equal(t, gloas.BuilderIndex(42), e.BuilderIndex, "only the root moves")
	})

	t.Run("envelope-builder-index", func(t *testing.T) {
		faults.SetForTest(t, faults.EnvelopeBuilderIndex)
		e := newEnvelope()
		require.True(t, applyEnvelopeFault(e))
		require.NotEqual(t, gloas.BuilderIndex(42), e.BuilderIndex)
		require.Equal(t, phase0.Root{0x02}, e.BeaconBlockRoot, "only the builder index moves")
	})
}
