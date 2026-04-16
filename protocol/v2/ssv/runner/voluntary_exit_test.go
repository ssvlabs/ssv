package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVoluntaryExitRunnerExpectedPreConsensusRootsHandlesMissingCurrentDuty(t *testing.T) {
	runner := &VoluntaryExitRunner{BaseRunner: &BaseRunner{State: &State{}}}

	require.NotPanics(t, func() {
		_, _, err := runner.expectedPreConsensusRootsAndDomain()
		require.ErrorContains(t, err, "current validator duty")
		require.ErrorContains(t, err, "current duty is nil")
	})
}
