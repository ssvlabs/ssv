//go:build alan_spec

package qbft

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	qbftimpl "github.com/ssvlabs/ssv/protocol/v2/qbft"
)

// preBooleForkNetConfig routes Proposer to the pre-Boole round-robin selection, which is
// what the Alan vectors encode.
type preBooleForkNetConfig struct{}

func (preBooleForkNetConfig) EstimatedEpochAtSlot(phase0.Slot) phase0.Epoch { return 0 }
func (preBooleForkNetConfig) BooleForkAtSlot(phase0.Slot) bool              { return false }

// runRoundRobinSpecTest drives our proposer selection (via the fork-selecting Proposer
// seam) against the Alan vectors, rather than the spec struct's own Run, so the node's
// pre-fork leader election stays pinned to the Alan spec.
func runRoundRobinSpecTest(t *testing.T, test *spectests.RoundRobinSpecTest) {
	require.Equal(t, len(test.Heights), len(test.Rounds))
	require.Equal(t, len(test.Heights), len(test.Proposers))

	committee := make([]spectypes.OperatorID, 0, len(test.Share.Committee))
	for _, op := range test.Share.Committee {
		committee = append(committee, op.OperatorID)
	}

	for i, height := range test.Heights {
		got := qbftimpl.Proposer(height, test.Rounds[i], committee, preBooleForkNetConfig{})
		require.EqualValues(t, test.Proposers[i], got)
	}
}
