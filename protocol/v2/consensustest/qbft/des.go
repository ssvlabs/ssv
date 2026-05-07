package qbft

import (
	"container/heap"
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// runDES executes one QBFT simulation under virtual time. Single-goroutine
// event loop with priority queue ordered by (timestamp, sequence) for
// determinism.
func runDES(cfg desConfig) (rawOutcome, error) {
	s := newSim(cfg)
	if err := s.start(); err != nil {
		return rawOutcome{}, err
	}
	s.runLoop()
	return s.outcome(), nil
}

type sim struct {
	cfg       desConfig
	rng       *mrand.Rand
	now       time.Duration
	queue     eventQueue
	seq       int64
	operators []ct.OperatorID
	state     map[ct.OperatorID]*opState
	trace     []ct.TraceEntry
}

// opState is one operator's QBFT state.
type opState struct {
	round           int
	roundTimerSeq   int64                       // sequence number of the active round-timer event (canceled by bumping)
	proposalsByRound map[int]map[ct.OperatorID][]byte // round → leader → proposed V (multiple if equivocation)
	preparedByRound map[int]map[ct.OperatorID][]byte // round → op → V they prepared on
	committedByRound map[int]map[ct.OperatorID][]byte // round → op → V they committed on
	rcByRound       map[int]map[ct.OperatorID]bool   // round → op → did round-change for this round
	decided         bool
	decidedV        []byte
	decidedRound    int
	decidedTime     time.Duration
	timedOut        bool // protocol gave up (rounds exhausted or beyond MaxRounds)
}

func newSim(cfg desConfig) *sim {
	N := cfg.N
	operators := make([]ct.OperatorID, N)
	for i := 0; i < N; i++ {
		operators[i] = ct.OperatorID(i + 1)
	}
	state := make(map[ct.OperatorID]*opState, N)
	for _, op := range operators {
		state[op] = &opState{
			round:           1,
			proposalsByRound: make(map[int]map[ct.OperatorID][]byte),
			preparedByRound: make(map[int]map[ct.OperatorID][]byte),
			committedByRound: make(map[int]map[ct.OperatorID][]byte),
			rcByRound:       make(map[int]map[ct.OperatorID]bool),
		}
	}
	return &sim{
		cfg:       cfg,
		rng:       mrand.New(mrand.NewSource(cfg.Seed)),
		operators: operators,
		state:     state,
	}
}

func (s *sim) start() error {
	// At BFT_start: round 1's leader broadcasts PROPOSE. Each non-byz
	// operator's round 1 timer arms.
	leader := s.leaderForRound(1)
	for _, op := range s.operators {
		s.armRoundTimer(op, 1, s.cfg.BFTStart)
	}
	if s.cfg.Byz.LeaderProposesAtRound(leader, 1) {
		// Honest leader emits PROPOSE; byz can suppress or equivocate.
		plans := s.cfg.Byz.ProposalPlanForRound(s, leader, 1, s.canonValue(1))
		for _, p := range plans {
			recipients := p.Recipients
			if recipients == nil {
				recipients = s.operators
			}
			for _, to := range recipients {
				s.scheduleDelivery(s.cfg.BFTStart, leader, to, ct.KindLeaderBroadcast,
					&evtProposeArrival{from: leader, to: to, round: 1, value: p.V})
			}
		}
	}
	return nil
}

func (s *sim) runLoop() {
	// Cap virtual time at slot duration + headroom; events past that are
	// dropped. This is the "cluster gives up" point.
	maxTime := s.cfg.BFTStart + s.cfg.RT*time.Duration(s.cfg.MaxRounds+1)
	for s.queue.Len() > 0 {
		e := heap.Pop(&s.queue).(*queueItem)
		if e.when > maxTime {
			break
		}
		s.now = e.when
		if s.cfg.TraceEnabled {
			s.trace = append(s.trace, ct.TraceEntry{When: e.when, Event: e.ev.describe()})
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

func (s *sim) scheduleDelivery(emitTime time.Duration, from, to ct.OperatorID, kind ct.MsgKind, ev event) {
	if !s.cfg.Byz.AllowDelivery(from, to, kind) {
		return
	}
	d := s.cfg.Byz.OverrideDelay(s.rng, from, to, kind)
	if d < 0 {
		d = s.cfg.Network.Delay(s.rng, from, to, kind)
	}
	s.schedule(emitTime+d, ev)
}

func (s *sim) outcome() rawOutcome {
	out := rawOutcome{
		decided:      false,
		decidedRound: -1,
		perOp:        make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace:        s.trace,
	}
	earliestT := time.Duration(-1)
	for _, op := range s.operators {
		st := s.state[op]
		oo := rawOpOutcome{decided: st.decided}
		if st.decided {
			oo.value = append([]byte(nil), st.decidedV...)
			oo.round = st.decidedRound
			oo.time = st.decidedTime
			if earliestT < 0 || st.decidedTime < earliestT {
				earliestT = st.decidedTime
				out.decided = true
				out.decidedValue = append([]byte(nil), st.decidedV...)
				out.decidedRound = st.decidedRound
				out.decisionTime = st.decidedTime
			}
		}
		switch {
		case st.timedOut:
			oo.err = "rounds exhausted"
		case !st.decided:
			// Sim ended before this op decided or hit MaxRounds (e.g.,
			// runLoop's maxTime cap fired mid-round). Distinguish from
			// "decided" so the framework's Terminated invariant holds.
			oo.err = "simulation ended mid-round"
		}
		out.perOp[op] = oo
	}
	return out
}

// armRoundTimer schedules a round-timeout event for `op` at round `roundNum`.
// Bumping roundTimerSeq invalidates prior timers — they fire but no-op on
// sequence mismatch (preserves heap-order determinism).
func (s *sim) armRoundTimer(op ct.OperatorID, roundNum int, baseTime time.Duration) {
	st := s.state[op]
	st.roundTimerSeq++
	mySeq := st.roundTimerSeq
	timeoutAt := baseTime + s.cfg.RT
	s.schedule(timeoutAt, &evtRoundTimeout{op: op, round: roundNum, mySeq: mySeq})
}

// leaderForRound returns the leader for `round` (1-indexed). Round-robin:
// (round - 1) mod N + 1.
func (s *sim) leaderForRound(round int) ct.OperatorID {
	return s.operators[(round-1)%s.cfg.N]
}

// canonValue returns the canonical V the round's leader would propose.
// Different per round so a round-change with fresh-V proposes a different V.
func (s *sim) canonValue(round int) []byte {
	return []byte(fmt.Sprintf("qbft-canon-V-round-%d", round))
}

// quorum returns the QBFT quorum threshold = 2f+1 = ⌈2N/3⌉ + 1 at f-bound.
func (s *sim) quorum() int { return 2*((s.cfg.N-1)/3) + 1 }

// recordPrepare adds (op preparing for round on V) to op's local preparedByRound.
func (st *opState) recordPrepare(round int, sender ct.OperatorID, value []byte) {
	if st.preparedByRound[round] == nil {
		st.preparedByRound[round] = make(map[ct.OperatorID][]byte)
	}
	if _, exists := st.preparedByRound[round][sender]; exists {
		return
	}
	st.preparedByRound[round][sender] = append([]byte(nil), value...)
}

func (st *opState) recordCommit(round int, sender ct.OperatorID, value []byte) {
	if st.committedByRound[round] == nil {
		st.committedByRound[round] = make(map[ct.OperatorID][]byte)
	}
	if _, exists := st.committedByRound[round][sender]; exists {
		return
	}
	st.committedByRound[round][sender] = append([]byte(nil), value...)
}

func (st *opState) recordProposal(round int, leader ct.OperatorID, value []byte) {
	if st.proposalsByRound[round] == nil {
		st.proposalsByRound[round] = make(map[ct.OperatorID][]byte)
	}
	st.proposalsByRound[round][leader] = append([]byte(nil), value...)
}

func (st *opState) recordRC(round int, sender ct.OperatorID) {
	if st.rcByRound[round] == nil {
		st.rcByRound[round] = make(map[ct.OperatorID]bool)
	}
	st.rcByRound[round][sender] = true
}

// hasPrepareQuorum reports whether op has seen ≥ qV PREPAREs for `round`
// on the same V; if so, returns the V.
func (st *opState) hasPrepareQuorum(round, qV int) ([]byte, bool) {
	prepares := st.preparedByRound[round]
	if len(prepares) < qV {
		return nil, false
	}
	counts := map[string]int{}
	for _, v := range prepares {
		counts[string(v)]++
	}
	for v, c := range counts {
		if c >= qV {
			return []byte(v), true
		}
	}
	return nil, false
}

// hasCommitQuorum is the same shape for COMMITs.
func (st *opState) hasCommitQuorum(round, qV int) ([]byte, bool) {
	commits := st.committedByRound[round]
	if len(commits) < qV {
		return nil, false
	}
	counts := map[string]int{}
	for _, v := range commits {
		counts[string(v)]++
	}
	for v, c := range counts {
		if c >= qV {
			return []byte(v), true
		}
	}
	return nil, false
}

// hasRCQuorum reports whether op has seen ≥ qV ROUND_CHANGEs for `round`.
func (st *opState) hasRCQuorum(round, qV int) bool {
	return len(st.rcByRound[round]) >= qV
}

