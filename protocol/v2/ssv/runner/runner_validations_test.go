package runner

import (
	"context"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The retry classification a runner applies to a message that reaches it before its duty started, or
// after the duty succeeded, is keyed on errors.Is against the runner sentinels. Those sentinels travel
// inside a coded spec error, so this pins that they stay reachable end to end: the proposer retries
// both cases, while the committee runner — whose per-slot runner never starts again — retries only
// the not-yet-started one.
func TestRetryClassification_SentinelsReachable(t *testing.T) {
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     1,
		Messages: []*spectypes.PartialSignatureMessage{{Signer: 1, ValidatorIndex: 1}},
	}
	ctx, logger := context.Background(), zap.NewNop()

	t.Run("proposer: no duty yet is retried", func(t *testing.T) {
		r := &ProposerRunner{BaseRunner: &BaseRunner{}}
		err := r.ProcessPreConsensus(ctx, logger, msgs)
		require.ErrorIs(t, err, ErrNoDutyAssigned)
		require.True(t, IsRetryable(err))
	})

	t.Run("proposer: duty already succeeded is retried", func(t *testing.T) {
		r := &ProposerRunner{BaseRunner: &BaseRunner{State: &State{Succeeded: true}}}
		err := r.ProcessPreConsensus(ctx, logger, msgs)
		require.ErrorIs(t, err, ErrRunningDutySucceeded)
		require.True(t, IsRetryable(err))
	})

	t.Run("committee: no duty yet is retried", func(t *testing.T) {
		r := &CommitteeRunner{BaseRunner: &BaseRunner{}}
		err := r.ProcessPostConsensus(ctx, logger, msgs)
		require.ErrorIs(t, err, ErrNoDutyAssigned)
		require.True(t, IsRetryable(err))
	})

	t.Run("committee: duty already succeeded is dropped", func(t *testing.T) {
		r := &CommitteeRunner{BaseRunner: &BaseRunner{State: &State{Succeeded: true}}}
		err := r.ProcessPostConsensus(ctx, logger, msgs)
		require.ErrorIs(t, err, ErrRunningDutySucceeded)
		require.False(t, IsRetryable(err))
	})
}
