package runner

import (
	"context"
	"testing"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TestBaseRunner_watchNonBeaconDutyDeadline covers the deadline watcher that turns a silent
// pre-consensus-only duty failure (no quorum / failed submission) into one operator-visible warning.
func TestBaseRunner_watchNonBeaconDutyDeadline(t *testing.T) {
	const (
		quorum      = uint64(3)
		warnSnippet = "did not complete by deadline"
	)
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleVoluntaryExit, Slot: 100}

	// Tiny slot duration so the ~2-slot deadline elapses in milliseconds. A fresh *Beacon is used
	// (Network embeds *Beacon by pointer) so the shared global config isn't mutated.
	newRunner := func() *BaseRunner {
		return &BaseRunner{
			NetworkConfig: &networkconfig.Network{
				Beacon: &networkconfig.Beacon{SlotDuration: 5 * time.Millisecond},
			},
		}
	}

	t.Run("warns when the duty does not complete by the deadline", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		b.watchNonBeaconDutyDeadline(context.Background(), zap.New(core), duty, quorum)

		require.Eventually(t, func() bool {
			return logs.FilterMessageSnippet(warnSnippet).Len() == 1
		}, time.Second, 2*time.Millisecond, "expected exactly one deadline warning")
	})

	t.Run("stays silent when the duty finishes before the deadline", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		b.watchNonBeaconDutyDeadline(context.Background(), zap.New(core), duty, quorum)
		// markDutyFinished closes this channel on successful completion.
		close(b.nonBeaconDeadlineDone)

		time.Sleep(50 * time.Millisecond) // well past the ~10ms deadline
		require.Zero(t, logs.FilterMessageSnippet(warnSnippet).Len(), "must not warn for a completed duty")
	})

	t.Run("stays silent when the context is cancelled", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		ctx, cancel := context.WithCancel(context.Background())
		b.watchNonBeaconDutyDeadline(ctx, zap.New(core), duty, quorum)
		cancel()

		time.Sleep(50 * time.Millisecond)
		require.Zero(t, logs.FilterMessageSnippet(warnSnippet).Len(), "must not warn on shutdown")
	})
}
