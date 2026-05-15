package psigs

import (
	"container/heap"
	mrand "math/rand"
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
