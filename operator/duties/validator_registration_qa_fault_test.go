package duties

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/qa/faults"
)

func TestGloasVRDeprecatedRespectsTheFault(t *testing.T) {
	// gloasVRDeprecated is the single seam both Gloas gates now call, so the fault is tested there
	// rather than through the whole handler.
	t.Run("honest node stops the heartbeat at the fork", func(t *testing.T) {
		require.True(t, gloasVRDeprecated(true))
		require.False(t, gloasVRDeprecated(false))
	})

	t.Run("vr-postfork keeps it running", func(t *testing.T) {
		faults.SetForTest(t, faults.VRPostFork)
		require.False(t, gloasVRDeprecated(true))
		require.False(t, gloasVRDeprecated(false))
	})
}
