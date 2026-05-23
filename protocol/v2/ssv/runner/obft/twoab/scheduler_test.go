package twoab

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	wire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

// captureHooks is a LifecycleHooks impl that records broadcasts by wire kind
// and tracks submit / missed-slot calls. Concurrency-safe (the host-validation
// drain + resolve loop may call concurrently in richer tests).
type captureHooks struct {
	candidate []byte

	mu         sync.Mutex
	broadcasts []wire.MessageKind
	submitted  []*twoabcore.Output
	missed     int
}

func (h *captureHooks) record(data []byte) {
	env, err := wire.Unwrap(data)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.broadcasts = append(h.broadcasts, env.Kind)
}

func (h *captureHooks) kinds() []wire.MessageKind {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]wire.MessageKind, len(h.broadcasts))
	copy(out, h.broadcasts)
	return out
}

func (h *captureHooks) lifecycle() *LifecycleHooks {
	return &LifecycleHooks{
		FetchCandidate: func(context.Context, phase0.Slot, int) ([]byte, error) { return h.candidate, nil },
		HostValidate:   func(context.Context, phase0.Slot, int, []byte) (bool, error) { return true, nil },
		Broadcast: func(_ context.Context, _ phase0.Slot, data []byte) error {
			h.record(data)
			return nil
		},
		SubmitOutput: func(_ context.Context, _ phase0.Slot, out *twoabcore.Output) error {
			h.mu.Lock()
			h.submitted = append(h.submitted, out)
			h.mu.Unlock()
			return nil
		},
		OnMissedSlot: func(context.Context, phase0.Slot, error) {
			h.mu.Lock()
			h.missed++
			h.mu.Unlock()
		},
	}
}

func TestNewScheduler_Validation(t *testing.T) {
	ctrl := newTestController(t, 1)
	_, err := NewScheduler(nil, (&captureHooks{}).lifecycle())
	require.ErrorContains(t, err, "nil Controller")
	_, err = NewScheduler(ctrl, &LifecycleHooks{}) // missing required hooks
	require.ErrorContains(t, err, "required")
}

// TestScheduler_FetchFireBroadcast drives one lone operator (op 1, leader of
// layer 0 at slot 0) through Phase 1 (fetch + self-feed + broadcast), Phase 2a
// (fire), and the resolve loop. It asserts the leader broadcasts its Phase-1
// bundle AND — via the resolve loop, the sole broadcaster — its Phase-2a
// KindValue (the dynamic-Phase-2b detection path). A lone op can't reach
// σ-quorum, so the slot misses on the ctx deadline.
func TestScheduler_FetchFireBroadcast(t *testing.T) {
	ctrl := newTestController(t, 1)
	hooks := &captureHooks{candidate: []byte("V0")}
	sched, err := NewScheduler(ctrl, hooks.lifecycle())
	require.NoError(t, err)

	const slot = phase0.Slot(0)
	_, err = ctrl.StartNewInstance(slot)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	require.NoError(t, sched.FetchAndBroadcastBundle(ctx, slot, 0))
	require.NoError(t, sched.FirePhase2a(slot))

	// Lone op: never reaches σ-quorum → loop runs until ctx deadline → miss.
	err = sched.ResolveAndSubmitOpportunistically(ctx, slot)
	require.Error(t, err)

	kinds := hooks.kinds()
	require.Contains(t, kinds, wire.KindPhase1Bundle, "leader broadcasts its Phase-1 bundle")
	require.Contains(t, kinds, wire.KindValue, "σ-eligible op broadcasts its Phase-2a KindValue via the resolve loop")

	// KindValue is broadcast exactly once (the emissionMarks dedup).
	var valueCount int
	for _, k := range kinds {
		if k == wire.KindValue {
			valueCount++
		}
	}
	require.Equal(t, 1, valueCount, "own emission broadcast exactly once (marks dedup)")

	hooks.mu.Lock()
	require.Empty(t, hooks.submitted, "lone op can't reach σ-quorum")
	require.Positive(t, hooks.missed, "OnMissedSlot fired on ctx deadline")
	hooks.mu.Unlock()
}
