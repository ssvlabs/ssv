package qbft

import (
	"container/heap"
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// QBFT event hierarchy. Each event handle returns new scheduledEvents to
// enqueue, mirroring the OBFT adapter's pattern.

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

// ---- evtProposeArrival: PROPOSE delivered to a receiver -----------------

type evtProposeArrival struct {
	from, to ct.OperatorID
	round    int
	value    []byte
}

func (e *evtProposeArrival) describe() string {
	return fmt.Sprintf("ProposeArrival[from=%d to=%d round=%d v=%s]",
		e.from, e.to, e.round, hashValue(e.value))
}

func (e *evtProposeArrival) handle(s *sim) []scheduledEvent {
	st := s.state[e.to]
	// Skip if op has already moved past this round.
	if st.round > e.round || st.decided {
		return nil
	}
	st.recordProposal(e.round, e.from, e.value)

	// Host validity check on the proposed V.
	if !s.cfg.Host.Validate(e.to, e.round, e.value) {
		// Operator considers this V invalid → do not PREPARE on it.
		return nil
	}

	// QBFT rule: detection of equivocation (two distinct PROPOSEs from the
	// same leader at the same round). On detection, do not PREPARE.
	props := st.proposalsByRound[e.round]
	if len(props) > 1 {
		// Multiple distinct PROPOSEs observed for the same round → don't
		// PREPARE; wait for round timeout.
		return nil
	}

	// Honest receiver broadcasts PREPARE on the proposed V (assuming they
	// haven't already prepared in this round — QBFT semantics: 1 PREPARE
	// per round per op).
	if _, already := st.preparedByRound[e.round][e.to]; already {
		return nil
	}
	// Record self-prepare locally too (for quorum counting at e.to).
	st.recordPrepare(e.round, e.to, e.value)

	if !s.cfg.Byz.AllowPrepareBroadcast(e.to, e.round) {
		return nil
	}
	var out []scheduledEvent
	for _, peer := range s.operators {
		if peer == e.to {
			continue
		}
		s.scheduleDelivery(s.now, e.to, peer, ct.KindCommit,
			&evtPrepareArrival{from: e.to, to: peer, round: e.round, value: e.value})
	}
	return out
}

// ---- evtPrepareArrival: PREPARE delivered to a receiver -----------------

type evtPrepareArrival struct {
	from, to ct.OperatorID
	round    int
	value    []byte
}

func (e *evtPrepareArrival) describe() string {
	return fmt.Sprintf("PrepareArrival[from=%d to=%d round=%d v=%s]",
		e.from, e.to, e.round, hashValue(e.value))
}

func (e *evtPrepareArrival) handle(s *sim) []scheduledEvent {
	st := s.state[e.to]
	if st.round > e.round || st.decided {
		return nil
	}
	st.recordPrepare(e.round, e.from, e.value)

	// Check PREPARE quorum.
	v, ok := st.hasPrepareQuorum(e.round, s.quorum())
	if !ok {
		return nil
	}
	// Already committed in this round → don't re-broadcast COMMIT.
	if _, alreadyCommitted := st.committedByRound[e.round][e.to]; alreadyCommitted {
		return nil
	}
	st.recordCommit(e.round, e.to, v)

	if !s.cfg.Byz.AllowCommitBroadcast(e.to, e.round) {
		return nil
	}
	var out []scheduledEvent
	for _, peer := range s.operators {
		if peer == e.to {
			continue
		}
		s.scheduleDelivery(s.now, e.to, peer, ct.KindCommit,
			&evtCommitArrival{from: e.to, to: peer, round: e.round, value: v})
	}
	return out
}

// ---- evtCommitArrival: COMMIT delivered to a receiver -------------------

type evtCommitArrival struct {
	from, to ct.OperatorID
	round    int
	value    []byte
}

func (e *evtCommitArrival) describe() string {
	return fmt.Sprintf("CommitArrival[from=%d to=%d round=%d v=%s]",
		e.from, e.to, e.round, hashValue(e.value))
}

func (e *evtCommitArrival) handle(s *sim) []scheduledEvent {
	st := s.state[e.to]
	if st.decided {
		return nil
	}
	st.recordCommit(e.round, e.from, e.value)

	v, ok := st.hasCommitQuorum(e.round, s.quorum())
	if !ok {
		return nil
	}

	// +1 BTT models post-consensus partial-sig collection (qV partial sigs
	// aggregated before the signed output is ready for relay submission).
	st.decided = true
	st.decidedV = append([]byte(nil), v...)
	st.decidedRound = e.round
	st.decidedTime = s.now + s.cfg.BTT
	return nil
}

// ---- evtRoundChangeArrival: ROUND_CHANGE delivered ----------------------

type evtRoundChangeArrival struct {
	from, to ct.OperatorID
	round    int // the round being abandoned (op is moving to round+1)
}

func (e *evtRoundChangeArrival) describe() string {
	return fmt.Sprintf("RoundChangeArrival[from=%d to=%d round=%d]", e.from, e.to, e.round)
}

func (e *evtRoundChangeArrival) handle(s *sim) []scheduledEvent {
	st := s.state[e.to]
	if st.decided {
		return nil
	}
	// Only count round-changes for the round the receiver is currently
	// in or about to be in.
	if e.round != st.round {
		return nil
	}
	st.recordRC(e.round, e.from)

	if !st.hasRCQuorum(e.round, s.quorum()) {
		return nil
	}
	// Quorum of ROUND_CHANGEs reached. Move to round + 1.
	newRound := e.round + 1
	if newRound > s.cfg.MaxRounds {
		st.timedOut = true
		return nil
	}
	return advanceToRound(s, e.to, newRound)
}

// advanceToRound transitions `op` to `newRound` and arms the timer / emits
// the leader's PROPOSE if op is the new leader.
func advanceToRound(s *sim, op ct.OperatorID, newRound int) []scheduledEvent {
	st := s.state[op]
	st.round = newRound
	s.armRoundTimer(op, newRound, s.now)

	if op != s.leaderForRound(newRound) {
		return nil
	}
	if !s.cfg.Byz.LeaderProposesAtRound(op, newRound) {
		return nil
	}
	plans := s.cfg.Byz.ProposalPlanForRound(s, op, newRound, s.canonValue(newRound))
	var out []scheduledEvent
	for _, p := range plans {
		recipients := p.Recipients
		if recipients == nil {
			recipients = s.operators
		}
		for _, to := range recipients {
			s.scheduleDelivery(s.now, op, to, ct.KindLeaderBroadcast,
				&evtProposeArrival{from: op, to: to, round: newRound, value: p.V})
		}
	}
	return out
}

// ---- evtRoundTimeout: round timer fires ---------------------------------

type evtRoundTimeout struct {
	op    ct.OperatorID
	round int
	mySeq int64 // matched against opState.roundTimerSeq; mismatch = no-op
}

func (e *evtRoundTimeout) describe() string {
	return fmt.Sprintf("RoundTimeout[op=%d round=%d]", e.op, e.round)
}

func (e *evtRoundTimeout) handle(s *sim) []scheduledEvent {
	st := s.state[e.op]
	// Stale timer (op moved to next round, decided, or armed a fresh timer).
	if st.roundTimerSeq != e.mySeq || st.decided || st.round != e.round {
		return nil
	}
	// Round timed out. Broadcast ROUND_CHANGE; receivers count toward
	// quorum and (if quorum) advance to round + 1.
	st.recordRC(e.round, e.op)
	if !s.cfg.Byz.AllowRoundChangeBroadcast(e.op, e.round) {
		return nil
	}
	var out []scheduledEvent
	for _, peer := range s.operators {
		if peer == e.op {
			continue
		}
		s.scheduleDelivery(s.now, e.op, peer, ct.KindRoundChange,
			&evtRoundChangeArrival{from: e.op, to: peer, round: e.round})
	}
	return out
}

// hashValue returns a hex-prefix of the value for diagnostic dumps.
func hashValue(v []byte) string {
	if len(v) == 0 {
		return "<empty>"
	}
	if len(v) > 6 {
		return fmt.Sprintf("%x", v[:6])
	}
	return fmt.Sprintf("%x", v)
}
