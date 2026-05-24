// Package desim is the shared discrete-event simulation engine for the
// consensustest protocol adapters (qbft / obft / twoab / psigs). It owns the
// virtual-time event loop, the priority queue, and the deterministic-driver
// state (clock, RNG, trace) that every protocol sim runs on, so that machinery
// lives in one place rather than being hand-duplicated per adapter.
//
// Each adapter keeps a package-local `sim` holding its protocol state; that
// sim embeds *Engine (gaining Now/Rng/Schedule/Run/Trace) and implements the
// Host interface (adding Mesh/Network/Bandwidth). Protocol events implement
// Event; the engine hands the Host to each event's Handle, and events recover
// their concrete sim with a single `s := h.(*sim)`.
package desim

import (
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Event is one scheduled simulation step. Handle runs the step against the
// host (the concrete sim) and returns any follow-on events to schedule;
// Describe is the human-readable label recorded into the trace.
type Event interface {
	Handle(Host) []Scheduled
	Describe() string
}

// Scheduled is an Event paired with its absolute virtual-time fire instant.
type Scheduled struct {
	When time.Duration
	Ev   Event
}

// Host is the engine + transport-substrate surface that events operate
// through. The Now/Rng/Schedule trio is provided by the embedded *Engine; the
// Mesh/Network/Bandwidth accessors are implemented by each adapter's sim.
type Host interface {
	Now() time.Duration
	Rng() *mrand.Rand
	Schedule(when time.Duration, ev Event)

	Mesh() *ct.MeshTopology
	Network() ct.NetworkModel
	Bandwidth() *ct.BandwidthReport
}

type queueItem struct {
	when time.Duration
	seq  int64
	ev   Event
}

// eventQueue is a typed min-heap of queueItems stored BY VALUE. It replaces
// the previous container/heap-based queue, which boxed each item into `any`
// for the heap.Interface and allocated a *queueItem per Schedule (the hot
// path: millions of events per stress run). Storing items by value in the
// backing slice removes the per-item heap allocation and the interface
// indirection.
//
// Ordering (less) is a strict TOTAL order — (when, then deprioritize
// false-before-true, then the unique seq) — so the pop sequence is identical
// to any other correct heap over the same comparator, preserving the
// framework's byte-identical-trace determinism guarantee regardless of the
// internal array layout.
type eventQueue struct {
	items []queueItem
	// deprioritize, when non-nil, refines equal-time ordering: events for
	// which it returns true sort AFTER events for which it returns false.
	deprioritize func(Event) bool
}

func (q *eventQueue) len() int { return len(q.items) }

func (q *eventQueue) less(i, j int) bool {
	a, b := &q.items[i], &q.items[j]
	if a.when != b.when {
		return a.when < b.when
	}
	if q.deprioritize != nil {
		da, db := q.deprioritize(a.ev), q.deprioritize(b.ev)
		if da != db {
			return !da // low-priority (true) sorts last
		}
	}
	return a.seq < b.seq
}

// push appends an item and sifts it up to restore the heap invariant.
func (q *eventQueue) push(it queueItem) {
	q.items = append(q.items, it)
	i := len(q.items) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !q.less(i, parent) {
			break
		}
		q.items[i], q.items[parent] = q.items[parent], q.items[i]
		i = parent
	}
}

// pop removes and returns the minimum item, sifting the moved last element
// down to restore the heap invariant.
func (q *eventQueue) pop() queueItem {
	n := len(q.items) - 1
	q.items[0], q.items[n] = q.items[n], q.items[0]
	top := q.items[n]
	q.items = q.items[:n]
	i := 0
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		smallest := l
		if r := l + 1; r < n && q.less(r, l) {
			smallest = r
		}
		if !q.less(smallest, i) {
			break
		}
		q.items[i], q.items[smallest] = q.items[smallest], q.items[i]
		i = smallest
	}
	return top
}

// Engine is the virtual-time event loop and its deterministic-driver state. A
// sim embeds *Engine and passes itself to Run as the Host.
type Engine struct {
	now          time.Duration
	rng          *mrand.Rand
	queue        eventQueue
	seq          int64
	trace        []ct.TraceEntry
	traceEnabled bool
}

// NewEngine returns an Engine seeded for determinism; (seed, scheduled events)
// fully determine the run. trace recording is gated on traceEnabled.
func NewEngine(seed int64, traceEnabled bool) *Engine {
	return &Engine{
		rng:          mrand.New(mrand.NewSource(seed)),
		traceEnabled: traceEnabled,
	}
}

// Now is the current virtual time (the fire instant of the event in flight).
func (e *Engine) Now() time.Duration { return e.now }

// Rng is the sim's deterministic RNG; all randomness must draw from it.
func (e *Engine) Rng() *mrand.Rand { return e.rng }

// Trace is the recorded event trace (non-nil only when traceEnabled).
func (e *Engine) Trace() []ct.TraceEntry { return e.trace }

// Tracef appends a diagnostic trace entry at the current virtual time, iff
// tracing is enabled. Adapters use it for sub-step diagnostics beyond the
// per-event line Run records automatically (e.g. a message-decode failure).
func (e *Engine) Tracef(format string, args ...any) {
	if !e.traceEnabled {
		return
	}
	e.trace = append(e.trace, ct.TraceEntry{When: e.now, Event: fmt.Sprintf(format, args...)})
}

// Schedule enqueues ev to fire at absolute virtual time `when`. The
// monotonically increasing seq breaks ties at equal `when` in insertion order,
// which is what makes a run byte-identical across repetitions.
func (e *Engine) Schedule(when time.Duration, ev Event) {
	e.seq++
	e.queue.push(queueItem{when: when, seq: e.seq, ev: ev})
}

// SetEqualTimeLowPriority installs a predicate consulted only at equal virtual
// time: events for which it returns true sort AFTER events for which it returns
// false (both still after any earlier-time event). Adapters use it for a
// required same-instant ordering — e.g. QBFT deprioritizes its round-timeout so
// a decision reachable the very instant the timer fires is processed first.
// Set it before scheduling; the default ordering is insertion order (seq).
func (e *Engine) SetEqualTimeLowPriority(pred func(Event) bool) {
	e.queue.deprioritize = pred
}

// Run drives the loop to quiescence. host is handed to every event's Handle
// (events type-assert it back to their concrete sim for protocol state).
// Events fire in (when, seq) order for tie-break determinism.
func (e *Engine) Run(host Host) { e.RunUntil(host, 0) }

// RunUntil drives the loop like Run but stops as soon as the next event would
// fire after maxTime (maxTime <= 0 means unbounded). The queue is time-ordered,
// so the first over-cap event implies all remaining are too — the loop breaks
// without processing it. Used by adapters (e.g. QBFT) that cap virtual time at
// a deadline-derived horizon rather than running to quiescence.
func (e *Engine) RunUntil(host Host, maxTime time.Duration) {
	for e.queue.len() > 0 {
		it := e.queue.pop()
		if maxTime > 0 && it.when > maxTime {
			break
		}
		e.now = it.when
		if e.traceEnabled {
			e.trace = append(e.trace, ct.TraceEntry{When: it.when, Event: it.ev.Describe()})
		}
		// Handle runs to completion before its returned events are scheduled,
		// so any event it schedules INLINE (e.g. QBFT's virtualNetwork
		// Broadcast during ProcessMsg) gets a lower seq than the events it
		// RETURNS here. At equal `when`, lower seq fires first (eventQueue.Less),
		// so inline-scheduled events are processed before returned ones from the
		// same Handle. Adapters depend on this ordering — preserve it.
		for _, ne := range it.ev.Handle(host) {
			e.Schedule(ne.When, ne.Ev)
		}
	}
}
