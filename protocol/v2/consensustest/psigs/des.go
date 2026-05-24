package psigs

import (
	"slices"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/desim"
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
// (to be translated to ct.Outcome by the caller). The shared desim.Engine
// runs the (when, seq)-ordered event loop for tie-break determinism.
func runDES(cfg desConfig) rawOutcome {
	s := newSim(cfg)
	s.start()
	s.Run(s)
	return s.outcome()
}

type sim struct {
	*desim.Engine
	cfg          desConfig
	operators    []ct.OperatorID
	mesh         *ct.MeshTopology
	threshold    int
	partialCount map[ct.OperatorID]int           // local partial-sig count (incl. self)
	decidedAt    map[ct.OperatorID]time.Duration // first moment op held qV partials
	// crashed[op] = true for completely-offline operators. Suppressed at
	// AllowSign / AllowDelivery; this set lets outcome() report them offline.
	crashed map[ct.OperatorID]bool
}

func newSim(cfg desConfig) *sim {
	N := cfg.N
	operators := make([]ct.OperatorID, N)
	copy(operators, cfg.Operators)
	crashed := make(map[ct.OperatorID]bool, len(cfg.Crashed))
	for _, op := range cfg.Crashed {
		crashed[op] = true
	}
	return &sim{
		Engine:       desim.NewEngine(cfg.Seed, cfg.TraceEnabled),
		cfg:          cfg,
		operators:    operators,
		mesh:         cfg.Mesh,
		threshold:    qV(N),
		partialCount: make(map[ct.OperatorID]int, N),
		decidedAt:    make(map[ct.OperatorID]time.Duration, N),
		crashed:      crashed,
	}
}

// Mesh / Network / Bandwidth satisfy desim.Host (Now/Rng/Schedule/Run/Trace
// come from the embedded *desim.Engine).
func (s *sim) Mesh() *ct.MeshTopology         { return s.mesh }
func (s *sim) Network() ct.NetworkModel       { return s.cfg.Network }
func (s *sim) Bandwidth() *ct.BandwidthReport { return s.cfg.Bandwidth }

// start schedules one evtPSigSign per honest operator at slot start
// (offset 0). The PSigs pipeline shifts wholesale with BFT_start, so the
// sim runs at BFT_start=0 and the report UI shifts decision times
// post-hoc. Byz operators that the byz pattern marks as non-signing are
// excluded from the initial schedule (they neither self-observe nor
// broadcast).
func (s *sim) start() {
	for _, op := range s.operators {
		if !s.cfg.Byz.AllowSign(op) {
			continue
		}
		s.Schedule(0, &evtPSigSign{op: op})
	}
	desim.ScheduleInitialHeartbeats(s, s.cfg.RelayCutoff)
}

// outcome aggregates per-op decision state into a rawOutcome. Cluster-
// wide DecisionTime is the earliest per-op decidedAt — matching the
// "any in-time op submits and the slot succeeds" semantic used by the
// other adapters.
func (s *sim) outcome() rawOutcome {
	out := rawOutcome{
		perOp: make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace: s.Trace(),
	}
	earliest := time.Duration(-1)
	for _, op := range s.operators {
		if s.crashed[op] {
			// Completely offline: never a decider, reported with Err="offline"
			// so Terminated stays satisfied and the op is excluded from
			// SingleV / HonestAgreement (decided-only checks).
			out.perOp[op] = rawOpOutcome{err: "offline"}
			continue
		}
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
//     and let the shared desim.MeshArrival handler re-flood downstream.
func (s *sim) emitToAll(from ct.OperatorID, kind ct.MsgKind, bytes int64, extraDelay time.Duration, build func(to ct.OperatorID) desim.Event) {
	if s.mesh != nil {
		s.emitMesh(from, kind, bytes, extraDelay, s.operators, build)
		return
	}
	s.emitDirect(from, kind, bytes, extraDelay, s.operators, build)
}

func (s *sim) emitDirect(from ct.OperatorID, kind ct.MsgKind, bytes int64, extraDelay time.Duration, recipients []ct.OperatorID, build func(to ct.OperatorID) desim.Event) {
	for _, to := range recipients {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, kind) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.Rng(), from, to, kind)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.Rng(), from, to, kind)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(from, to, kind, -1, bytes)
		}
		s.Schedule(s.Now()+delay+extraDelay, build(to))
	}
}

func (s *sim) emitMesh(from ct.OperatorID, kind ct.MsgKind, bytes int64, extraDelay time.Duration, recipients []ct.OperatorID, build func(to ct.OperatorID) desim.Event) {
	// Crashed operators are absent from the mesh topology — guard before
	// NodeForOperator (which would panic on a missing op) and emit nothing.
	if s.crashed[from] {
		return
	}
	mesh := s.mesh
	fromNode := mesh.NodeForOperator(from)
	id := mesh.NewMsgID()
	mesh.MarkSeen(fromNode, id)
	desim.CacheArrivalForGossip(s, fromNode, fromNode, id, kind, -1, bytes, build)
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
		delay := mesh.SampleHopDelay(s.Rng(), fromEP, mesh.EndpointFor(neighbor), kind)
		mesh.RecordMeshHop(s.cfg.Bandwidth, fromNode, neighbor, kind, -1, bytes)
		s.Schedule(s.Now()+delay+extraDelay, &desim.MeshArrival{
			From:      fromNode,
			To:        neighbor,
			Publisher: fromNode,
			MsgID:     id,
			Kind:      kind,
			Layer:     -1,
			Bytes:     bytes,
			Builder:   build,
		})
	}
}
