package runner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TestBaseRunner_watchDutyOutcome covers the per-duty outcome watcher: it reports every duty's
// terminal outcome exactly once, warning for the outcomes worth an operator's attention (a terminal
// failure, or a duty that was silently abandoned and missed the slot deadline) and staying quiet for
// a duty that concluded successfully.
func TestBaseRunner_watchDutyOutcome(t *testing.T) {
	const deadlineSnippet = "did not complete before slot end"
	const failedSnippet = "duty failed"

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

	t.Run("warns stuck when the duty does not conclude before slot end", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		b.watchDutyOutcome(context.Background(), zap.New(core))

		require.Eventually(t, func() bool {
			return logs.FilterMessageSnippet(deadlineSnippet).Len() == 1
		}, time.Second, 5*time.Millisecond, "expected exactly one deadline warning")
	})

	t.Run("proposer preferences: stuck horizon extends to the proposal slot start", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()
		b.RunnerRoleType = spectypes.RoleProposerPreferences
		// duty.Slot is a future proposal slot (slot 4 → its start is 4 slots away); the §5 duty keeps
		// converging until then, so the current slot's end must not report it stuck.
		b.State = &State{CurrentDuty: &spectypes.ValidatorDuty{Slot: 4}}

		b.watchDutyOutcome(context.Background(), zap.New(core))

		time.Sleep(120 * time.Millisecond) // two slots past the emission slot's end
		require.Zero(t, logs.FilterMessageSnippet(deadlineSnippet).Len(), "§5 must not report stuck before its proposal slot")

		require.Eventually(t, func() bool {
			return logs.FilterMessageSnippet(deadlineSnippet).Len() == 1
		}, time.Second, 5*time.Millisecond, "expected the stuck warning at the proposal slot's start")
	})

	t.Run("warns when the duty fails before slot end", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		b.watchDutyOutcome(context.Background(), zap.New(core))
		b.dutyConcluded <- dutyConclusion{outcome: dutyOutcomeFailed, reason: errors.New("boom")}

		require.Eventually(t, func() bool {
			return logs.FilterMessageSnippet(failedSnippet).Len() == 1
		}, time.Second, 5*time.Millisecond, "expected a failure warning")
		require.Zero(t, logs.FilterMessageSnippet(deadlineSnippet).Len(), "a reported duty must not also warn about a deadline")
	})

	t.Run("stays silent when the duty succeeds before slot end", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		b.watchDutyOutcome(context.Background(), zap.New(core))
		b.dutyConcluded <- dutyConclusion{outcome: dutyOutcomeSucceeded}

		time.Sleep(100 * time.Millisecond) // well past the slot end
		require.Zero(t, logs.Len(), "must not warn for a successfully concluded duty")
	})

	t.Run("stays silent when the context is canceled", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		ctx, cancel := context.WithCancel(context.Background())
		b.watchDutyOutcome(ctx, zap.New(core))
		cancel()

		time.Sleep(100 * time.Millisecond)
		require.Zero(t, logs.Len(), "must not warn on shutdown")
	})

	t.Run("does not report a duty aborted by cancellation", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		b := newRunner()

		// A duty aborted by shutdown returns context.Canceled; markDutyFailed drops it (a cancellation
		// is not a failure), so no conclusion is produced and ctx.Done() unblocks the watcher silently.
		ctx, cancel := context.WithCancel(context.Background())
		b.watchDutyOutcome(ctx, zap.New(core))
		cancel()
		b.markDutyFailed(context.Canceled)

		time.Sleep(100 * time.Millisecond)
		require.Zero(t, logs.Len(), "a duty aborted by cancellation must not be reported")
	})
}

// TestBaseRunner_markDutyOutcomes pins what each marker records: succeeded/not_required are full
// completions (State.Succeeded set) reported under distinct outcomes, failed leaves State.Succeeded
// false and carries its reason, and a second conclusion is a no-op.
func TestBaseRunner_markDutyOutcomes(t *testing.T) {
	newRunner := func() *BaseRunner {
		return &BaseRunner{
			State:         &State{},
			dutyConcluded: make(chan dutyConclusion, 1),
		}
	}

	t.Run("markDutySucceeded reports succeeded and sets State.Succeeded", func(t *testing.T) {
		b := newRunner()
		ch := b.dutyConcluded

		b.markDutySucceeded()

		require.True(t, b.State.Succeeded, "duty should be marked succeeded")
		require.Nil(t, b.dutyConcluded, "channel reference should be cleared (idempotency)")
		require.Equal(t, dutyOutcomeSucceeded, (<-ch).outcome)
	})

	t.Run("markDutyNotRequired reports not_required and sets State.Succeeded", func(t *testing.T) {
		b := newRunner()
		ch := b.dutyConcluded

		b.markDutyNotRequired()

		require.True(t, b.State.Succeeded, "a not-required duty is still a correct completion")
		require.Equal(t, dutyOutcomeNotRequired, (<-ch).outcome)
	})

	t.Run("markDutyFailed reports failed with reason and does not set State.Succeeded", func(t *testing.T) {
		b := newRunner()
		ch := b.dutyConcluded
		boom := errors.New("boom")

		b.markDutyFailed(boom)

		require.False(t, b.State.Succeeded, "a failed duty must not be marked succeeded")
		c := <-ch
		require.Equal(t, dutyOutcomeFailed, c.outcome)
		require.Equal(t, boom, c.reason)
	})

	t.Run("markDutyFailed drops a context.Canceled reason (a cancellation is not a failure)", func(t *testing.T) {
		b := newRunner()

		b.markDutyFailed(context.Canceled)
		b.markDutyFailed(fmt.Errorf("submit: %w", context.Canceled)) // wrapped still matches

		require.NotNil(t, b.dutyConcluded, "no conclusion should have been produced")
		require.Empty(t, b.dutyConcluded, "a cancellation must not be recorded as a failure")
	})

	t.Run("a second conclusion is a no-op", func(t *testing.T) {
		b := newRunner()
		b.markDutyFailed(errors.New("first"))
		require.NotPanics(t, func() { b.markDutyFailed(errors.New("second")) }, "second conclusion must not send again")
	})
}
