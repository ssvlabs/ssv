package psigs

import (
	"container/heap"
	mrand "math/rand"
	"slices"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// preAgreedValue is the V every operator signs. PSigs has no consensus
// on V (it's a baseline-measurement protocol), so any constant byte
// string works for the purpose of comparing to OBFT/QBFT decided
// values. Chosen at fixed bytes to make trace inspection easy.
var preAgreedValue = []byte("psigs-pre-agreed-V")

// qV computes the partial-sig threshold for cluster size N. Mirrors the
// 2f+1 quorum convention used by OBFT/2abOBFT/QBFT.
func qV(N int) int {
	f := (N - 1) / 3
	return 2*f + 1
}

// runDES drives the PSigs simulator end-to-end and returns the rawOutcome
// (to be translated to ct.Outcome by the caller). Single-goroutine event
// loop ordered by (when, seq) for tie-break determinism.
func runDES(cfg desConfig) rawOutcome {
	s := newSim(cfg)
	s.start()
	s.runLoop()
	return s.outcome()
}

type sim struct {
	cfg          desConfig
	rng          *mrand.Rand
	now          time.Duration
	queue        eventQueue
	seq          int64
	operators    []ct.OperatorID
	mesh         *ct.MeshTopology
	threshold    int
	partialCount map[ct.OperatorID]int           // local partial-sig count (incl. self)
	decidedAt    map[ct.OperatorID]time.Duration // first moment op held qV partials
	trace        []ct.TraceEntry
}

func newSim(cfg desConfig) *sim {
	N := cfg.N
	operators := make([]ct.OperatorID, N)
	copy(operators, cfg.Operators)
	return &sim{
		cfg:          cfg,
		rng:          mrand.New(mrand.NewSource(cfg.Seed)),
		operators:    operators,
		mesh:         cfg.Mesh,
		threshold:    qV(N),
		partialCount: make(map[ct.OperatorID]int, N),
		decidedAt:    make(map[ct.OperatorID]time.Duration, N),
	}
}

// start schedules one evtPSigSign per honest operator at SlotStart.
// Byz operators that the byz pattern marks as non-signing are excluded
// from the initial schedule (they neither self-observe nor broadcast).
func (s *sim) start() {
	for _, op := range s.operators {
		if !s.cfg.Byz.AllowSign(op) {
			continue
		}
		s.schedule(s.cfg.SlotStart, &evtPSigSign{op: op})
	}
	s.scheduleInitialHeartbeats()
}

func (s *sim) runLoop() {
	for s.queue.Len() > 0 {
		e := heap.Pop(&s.queue).(*queueItem)
		s.now = e.when
		if s.cfg.TraceEnabled {
			s.trace = append(s.trace, ct.TraceEntry{
				When:  e.when,
				Event: e.ev.describe(),
			})
		}
		newEvents := e.ev.handle(s)
		for _, ne := range newEvents {
			s.schedule(ne.when, ne.ev)
		}
	}
}

func (s *sim) schedule(when time.Duration, ev event) {
	s.seq++
	heap.Push(&s.queue, &queueItem{when: when, seq: s.seq, ev: ev})
}

// outcome aggregates per-op decision state into a rawOutcome. Cluster-
// wide DecisionTime is the earliest per-op decidedAt — matching the
// "any in-time op submits and the slot succeeds" semantic used by the
// other adapters.
func (s *sim) outcome() rawOutcome {
	out := rawOutcome{
		perOp: make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace: s.trace,
	}
	earliest := time.Duration(-1)
	for _, op := range s.operators {
		o := rawOpOutcome{}
		if t, ok := s.decidedAt[op]; ok {
			o.decided = true
			o.value = append([]byte(nil), preAgreedValue...)
			o.time = t
			if earliest < 0 || t < earliest {
				earliest = t
				out.decided = true
				out.decisionTime = t
				out.decidedValue = append([]byte(nil), preAgreedValue...)
			}
		}
		out.perOp[op] = o
	}
	return out
}

// ---- emit helpers: select Direct or Mesh per cfg.Mesh -----------------

// emitToAll routes a single-broadcast message from `from` to every other
// op in the cluster, honoring byz delivery / delay overrides. Transport:
//   - cfg.Mesh == nil (DeliveryDirect): per-recipient fanout against
//     cfg.Network with per-(from, to) byz checks.
//   - cfg.Mesh != nil (DeliveryMesh): publish to `from`'s mesh neighbors
//     and let evtMeshArrival re-flood downstream.
func (s *sim) emitToAll(from ct.OperatorID, kind ct.MsgKind, bytes int64, extraDelay time.Duration, build func(to ct.OperatorID) event) {
	if s.mesh != nil {
		s.emitMesh(from, kind, bytes, extraDelay, s.operators, build)
		return
	}
	s.emitDirect(from, kind, bytes, extraDelay, s.operators, build)
}

func (s *sim) emitDirect(from ct.OperatorID, kind ct.MsgKind, bytes int64, extraDelay time.Duration, recipients []ct.OperatorID, build func(to ct.OperatorID) event) {
	for _, to := range recipients {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, kind) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.rng, from, to, kind)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, from, to, kind)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(from, to, kind, -1, bytes)
		}
		s.schedule(s.now+delay+extraDelay, build(to))
	}
}

func (s *sim) emitMesh(from ct.OperatorID, kind ct.MsgKind, bytes int64, extraDelay time.Duration, recipients []ct.OperatorID, build func(to ct.OperatorID) event) {
	mesh := s.mesh
	fromNode := mesh.NodeForOperator(from)
	id := mesh.NewMsgID()
	mesh.MarkSeen(fromNode, id)
	s.cacheArrivalForGossip(fromNode, fromNode, id, kind, bytes, build)
	fromEP := mesh.EndpointFor(fromNode)
	for _, neighbor := range mesh.Neighbors(fromNode) {
		isProto := mesh.IsProtocol(neighbor)
		if isProto {
			toOp := mesh.OperatorForNode(neighbor)
			if !slices.Contains(recipients, toOp) {
				continue
			}
			if !s.cfg.Byz.AllowDelivery(from, toOp, kind) {
				continue
			}
		}
		delay := mesh.SampleHopDelay(s.rng, fromEP, mesh.EndpointFor(neighbor), kind)
		mesh.RecordMeshHop(s.cfg.Bandwidth, fromNode, neighbor, kind, -1, bytes)
		s.schedule(s.now+delay+extraDelay, &evtMeshArrival{
			from:      fromNode,
			to:        neighbor,
			publisher: fromNode,
			msgID:     id,
			kind:      kind,
			bytes:     bytes,
			builder:   build,
		})
	}
}

// cacheArrivalForGossip mirrors the OBFT helper of the same name. See
// protocol/v2/consensustest/obft/des.go cacheArrivalForGossip for the
// rationale. PSigs-specific: the kind doesn't have per-layer / per-
// round metadata, so RecordMeshHop receives layer=-1 (and the helper
// signature is correspondingly shorter than OBFT's).
func (s *sim) cacheArrivalForGossip(
	cacheOwner, publisher ct.MeshNode,
	msgID ct.MsgID, kind ct.MsgKind, bytes int64,
	builder func(to ct.OperatorID) event,
) {
	mesh := s.mesh
	g := mesh.Gossip()
	if !g.Enabled {
		return
	}
	mesh.MCacheInsert(cacheOwner, msgID, ct.MCacheEntry{
		Kind:  kind,
		Bytes: bytes,
		Reinject: func(requester ct.MeshNode) {
			respEP := mesh.EndpointFor(cacheOwner)
			reqEP := mesh.EndpointFor(requester)
			delay := s.cfg.Network.Delay(s.rng, respEP, reqEP, kind)
			mesh.RecordMeshHop(s.cfg.Bandwidth, cacheOwner, requester, kind, -1, bytes)
			s.schedule(s.now+delay, &evtMeshArrival{
				from:      cacheOwner,
				to:        requester,
				publisher: publisher,
				msgID:     msgID,
				kind:      kind,
				bytes:     bytes,
				builder:   builder,
			})
		},
	}, g.HistoryLength)
}

// scheduleInitialHeartbeats mirrors the OBFT helper. See
// protocol/v2/consensustest/obft/des.go scheduleInitialHeartbeats for
// the rationale; identical body modulo the typed event.
func (s *sim) scheduleInitialHeartbeats() {
	mesh := s.mesh
	if mesh == nil {
		return
	}
	g := mesh.Gossip()
	if !g.Enabled {
		return
	}
	total := mesh.TotalNodes()
	if total <= 0 {
		return
	}
	phase := g.HeartbeatInterval / time.Duration(total)
	for i := 0; i < total; i++ {
		nodeOffset := time.Duration(i) * phase
		for tick := time.Duration(0); ; tick += g.HeartbeatInterval {
			at := nodeOffset + tick
			if at > s.cfg.RelayCutoff {
				break
			}
			s.schedule(at, &evtMeshHeartbeat{node: ct.MeshNode(i)})
		}
	}
}
