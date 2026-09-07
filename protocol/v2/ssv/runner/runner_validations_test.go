package runner

import (
	"context"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A partial that reaches a runner before its duty started, or after the duty succeeded, is reported
// through the two duty-state sentinels and is not retried: the duty queue already holds such messages
// while no duty runs (see ValidatePreConsensusMsg). For every runner, pre- and post-consensus, the
// sentinel is reachable to errors.Is, the spec code to errors.As, and the error is not retryable.
func TestDutyStateSentinels_NotRetried(t *testing.T) {
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     1,
		Messages: []*spectypes.PartialSignatureMessage{{Signer: 1, ValidatorIndex: 1}},
	}
	ctx, logger := context.Background(), zap.NewNop()
	succeeded := func() *BaseRunner {
		state := NewRunnerState(3, &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: 1})
		state.Succeeded = true
		return &BaseRunner{State: state}
	}

	runners := []struct {
		name    string
		process func(*BaseRunner) error
	}{
		{"proposer pre-consensus", func(b *BaseRunner) error {
			return (&ProposerRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"proposer post-consensus", func(b *BaseRunner) error {
			return (&ProposerRunner{BaseRunner: b}).ProcessPostConsensus(ctx, logger, msgs)
		}},
		{"aggregator pre-consensus", func(b *BaseRunner) error {
			return (&AggregatorRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"sync-committee contribution pre-consensus", func(b *BaseRunner) error {
			return (&SyncCommitteeAggregatorRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"validator registration pre-consensus", func(b *BaseRunner) error {
			return (&ValidatorRegistrationRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"voluntary exit pre-consensus", func(b *BaseRunner) error {
			return (&VoluntaryExitRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"committee post-consensus", func(b *BaseRunner) error {
			return (&CommitteeRunner{BaseRunner: b}).ProcessPostConsensus(ctx, logger, msgs)
		}},
	}
	for _, r := range runners {
		t.Run(r.name, func(t *testing.T) {
			err := r.process(&BaseRunner{})
			require.ErrorIs(t, err, ErrNoDutyAssigned)
			requireSpecCode(t, err, spectypes.NoRunningDutyErrorCode)
			require.False(t, IsRetryable(err))

			err = r.process(succeeded())
			require.ErrorIs(t, err, ErrRunningDutySucceeded)
			requireSpecCode(t, err, spectypes.NoRunningDutyErrorCode)
			require.False(t, IsRetryable(err))
		})
	}
}

func requireSpecCode(t *testing.T, err error, code int) {
	t.Helper()
	var specErr *spectypes.Error
	require.ErrorAs(t, err, &specErr)
	require.Equal(t, code, specErr.Code)
}
