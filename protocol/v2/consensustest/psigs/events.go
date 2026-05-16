package psigs

import (
	"container/heap"
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// ---- Event-queue primitives (mirror OBFT/QBFT adapter shape) -----------

type event interface {
	handle(s *sim) []scheduledEvent
	describe() string
}

type scheduledEvent struct {
	when time.Duration
	ev   event
}

type queueItem struct {
	when time.Duration
	seq  int64
	ev   event
}

type eventQueue []*queueItem

func (q eventQueue) Len() int { return len(q) }
func (q eventQueue) Less(i, j int) bool {
	if q[i].when != q[j].when {
		return q[i].when < q[j].when
	}
	return q[i].seq < q[j].seq
}
func (q eventQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *eventQueue) Push(x any)   { *q = append(*q, x.(*queueItem)) }
func (q *eventQueue) Pop() any {
	old := *q
	n := len(old)
	x := old[n-1]
	*q = old[:n-1]
	return x
}

var _ heap.Interface = (*eventQueue)(nil)

// pSigBytes is the wire size of a single partial-sig message for
// bandwidth accounting. Models a BLS partial sig (96 bytes) + minimal
// envelope wrapping (slot, op-id, message type, op signature ≈ 160
// bytes). Matches the order of magnitude of QBFT's post-consensus
// partial-sig wire footprint.
const pSigBytes int64 = 96 + 160

// ---- evtPSigSign: operator signs V at SlotStart and broadcasts --------

// evtPSigSign fires once per honest operator at SlotStart. The op self-
// observes (count = 1) and broadcasts the partial sig to every peer.
// The local self-observation can already satisfy qV at f=0 (degenerate
// n=1 single-op "cluster" where qV=1) — we record the decision time
// here to cover that edge case; for realistic f≥1 the count increments
// via evtPSigArrival as peers' partials land.
type evtPSigSign struct {
	op ct.OperatorID
}

func (e *evtPSigSign) describe() string {
	return fmt.Sprintf("PSigSign[op=%d]", e.op)
}

func (e *evtPSigSign) handle(s *sim) []scheduledEvent {
	s.partialCount[e.op] = 1
	if s.partialCount[e.op] >= s.threshold {
		s.decidedAt[e.op] = s.now
	}
	// Byz patterns can delay an operator's own broadcast (ByzDelayedCommit)
	// so the partial-sig lands at peers past the soft target. extraDelay is
	// added on top of the per-pair network delay.
	extraDelay := s.cfg.Byz.OverrideOwnSignDispatchDelay(e.op)
	s.emitToAll(e.op, ct.KindPostConsensus, pSigBytes, extraDelay, func(to ct.OperatorID) event {
		return &evtPSigArrival{from: e.op, to: to}
	})
	return nil
}

// ---- evtPSigArrival: a peer's partial sig lands at the receiver -------

type evtPSigArrival struct {
	from, to ct.OperatorID
}

func (e *evtPSigArrival) describe() string {
	return fmt.Sprintf("PSigArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtPSigArrival) handle(s *sim) []scheduledEvent {
	// Early-exit once the receiver has already reached qV — additional
	// partials don't change the decided time, and skipping them keeps the
	// partialCount bounded by qV. Mesh re-flood is already dedup'd by
	// MeshTopology.MarkSeen in evtMeshArrival, and direct mode schedules
	// each (from, to) exactly once, so this guard is purely an
	// optimization (no risk of double-counting a single partial).
	if _, already := s.decidedAt[e.to]; already {
		return nil
	}
	s.partialCount[e.to]++
	if s.partialCount[e.to] >= s.threshold {
		s.decidedAt[e.to] = s.now
	}
	return nil
}

// ---- evtMeshArrival: per-hop delivery in DeliveryMesh mode ------------

// evtMeshArrival mirrors the OBFT / QBFT adapters' mesh delivery event.
// See protocol/v2/consensustest/obft/events.go evtMeshArrival for the
// design rationale (including the publisher-skip in the forward loop).
// PSigs-specific: the only message kind in flight is KindPostConsensus
// (a partial-sig); on first arrival at a cluster neighbor, the builder
// constructs an evtPSigArrival.
type evtMeshArrival struct {
	from, to  ct.MeshNode
	publisher ct.MeshNode
	msgID     ct.MsgID
	kind      ct.MsgKind
	bytes     int64
	builder   func(to ct.OperatorID) event
}

func (e *evtMeshArrival) describe() string {
	return ct.FormatMeshArrival(e.from, e.to, e.publisher, e.msgID, e.kind)
}

func (e *evtMeshArrival) handle(s *sim) []scheduledEvent {
	mesh := s.mesh
	if !mesh.MarkSeen(e.to, e.msgID) {
		return nil
	}
	var out []scheduledEvent
	if mesh.IsProtocol(e.to) {
		recipientOp := mesh.OperatorForNode(e.to)
		out = append(out, scheduledEvent{
			when: s.now + mesh.ValidateDelay(),
			ev:   e.builder(recipientOp),
		})
	}
	fromEP := mesh.EndpointFor(e.to)
	for _, neighbor := range mesh.Neighbors(e.to) {
		if neighbor == e.from || neighbor == e.publisher {
			continue
		}
		delay := mesh.SampleHopDelay(s.rng, fromEP, mesh.EndpointFor(neighbor), e.kind)
		mesh.RecordMeshHop(s.cfg.Bandwidth, e.to, neighbor, e.kind, -1, e.bytes)
		out = append(out, scheduledEvent{
			when: s.now + mesh.ValidateDelay() + delay,
			ev: &evtMeshArrival{
				from:      e.to,
				to:        neighbor,
				publisher: e.publisher,
				msgID:     e.msgID,
				kind:      e.kind,
				bytes:     e.bytes,
				builder:   e.builder,
			},
		})
	}
	return out
}
