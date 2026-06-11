package runner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TestBaseRunner_watchDutyDeadline covers the duty-completion watcher that turns a silently
// abandoned duty (of any kind) into one operator-visible warning at slot end.
func TestBaseRunner_watchDutyDeadline(t *testing.T) {
	const warnSnippet = "did not complete before slot end"

	// Genesis is set to now so the watcher starts at the beginning of slot 0 and its deadline
	// (end of the current slot) is a full slot away. A fresh *Beacon is used (Network embeds
	// *Beacon by pointer) so the shared global config isn't mutated.
	newRunner := func() *BaseRunner {
		return &BaseRunner{
			NetworkConfig: &networkconfig.Network{
				Beacon: &networkconfig.Beacon{
					GenesisTime:  time.Now(),
					SlotDuration: 50 * time.Millisecond,
				},
			},
		}
	}

	t.Run("warns when the duty does not complete before slot end", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		b.watchDutyFinishedOnTime(context.Background(), zap.New(core))

		require.Eventually(t, func() bool {
			return logs.FilterMessageSnippet(warnSnippet).Len() == 1
		}, time.Second, 5*time.Millisecond, "expected exactly one deadline warning")
	})

	t.Run("stays silent when the duty finishes before slot end", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		b.watchDutyFinishedOnTime(context.Background(), zap.New(core))
		// markDutyFinished closes this channel on duty completion.
		close(b.dutyFinishedOnTime)

		time.Sleep(100 * time.Millisecond) // well past the slot end
		require.Zero(t, logs.FilterMessageSnippet(warnSnippet).Len(), "must not warn for a completed duty")
	})

	t.Run("stays silent when the context is canceled", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		ctx, cancel := context.WithCancel(context.Background())
		b.watchDutyFinishedOnTime(ctx, zap.New(core))
		cancel()

		time.Sleep(100 * time.Millisecond)
		require.Zero(t, logs.FilterMessageSnippet(warnSnippet).Len(), "must not warn on shutdown")
	})
}
