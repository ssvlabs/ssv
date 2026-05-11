package qbft

import (
	"container/heap"
	"context"
	"fmt"
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

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

// ---- evtStartInstance: triggers Instance.Start for one honest op ---------

type evtStartInstance struct {
	op spectypes.OperatorID
}

func (e *evtStartInstance) describe() string {
	return fmt.Sprintf("StartInstance[op=%d]", e.op)
}

func (e *evtStartInstance) handle(s *sim) []scheduledEvent {
	inst := s.instances[e.op]
	if inst == nil {
		return nil
	}
	checker := newVirtualValueChecker(s, ct.OperatorID(e.op))
	inst.Start(context.Background(), s.startValue, checker)
	return nil
}

// ---- evtMessageArrival: SignedSSVMessage delivered to a receiver ---------

type evtMessageArrival struct {
	from, to ct.OperatorID
	msg      *spectypes.SignedSSVMessage
}

func (e *evtMessageArrival) describe() string {
	return fmt.Sprintf("MessageArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtMessageArrival) handle(s *sim) []scheduledEvent {
	op := spectypes.OperatorID(e.to)
	inst := s.instances[op]
	if inst == nil {
		return nil
	}
	if !inst.CanProcessMessages() {
		return nil
	}
	pm, err := specqbft.NewProcessingMessage(e.msg)
	if err != nil {
		if s.cfg.TraceEnabled {
			s.trace = append(s.trace, ct.TraceEntry{
				When:  s.now,
				Event: fmt.Sprintf("MessageDecode-FAILED[from=%d to=%d err=%v]", e.from, e.to, err),
			})
		}
		return nil
	}
	// Stash the in-flight round so virtualValueChecker.CheckValue (called
	// inside ProcessMsg → uponProposal → isProposalJustification) can report
	// the proposal's round to the host instead of Instance.State.Round (which
	// hasn't been bumped yet). Cleared after ProcessMsg returns.
	s.inflightRound[op] = pm.QBFTMessage.Round
	decided, decidedValue, _, err := inst.ProcessMsg(context.Background(), zap.NewNop(), pm)
	delete(s.inflightRound, op)
	if err != nil && s.cfg.TraceEnabled {
		s.trace = append(s.trace, ct.TraceEntry{
			When: s.now,
			Event: fmt.Sprintf("ProcessMsg-rejected[from=%d to=%d type=%d round=%d err=%v]",
				e.from, e.to, pm.QBFTMessage.MsgType, pm.QBFTMessage.Round, err),
		})
	}
	if decided {
		s.recordDecided(op, pm.QBFTMessage.Round, decidedValue)
	}
	return nil
}

// ---- evtRoundTimeout: virtual round timer fires --------------------------

type evtRoundTimeout struct {
	op    spectypes.OperatorID
	round specqbft.Round
	mySeq int64
}

func (e *evtRoundTimeout) describe() string {
	return fmt.Sprintf("RoundTimeout[op=%d round=%d]", e.op, e.round)
}

func (e *evtRoundTimeout) handle(s *sim) []scheduledEvent {
	timer := s.timers[e.op]
	if timer == nil || timer.seq != e.mySeq {
		return nil // stale (rearmed by a newer round or stopped)
	}
	inst := s.instances[e.op]
	if inst == nil || !inst.CanProcessMessages() {
		return nil
	}
	if dec, _ := inst.IsDecided(); dec {
		return nil
	}
	// Real Instance handles round-change construction + broadcast. After
	// UponRoundTimeout, State.Round is bumped and a ROUND_CHANGE is on the
	// wire. The new-round leader's PROPOSE comes via uponRoundChange at
	// receivers (when quorum reaches) — for honest leaders the Instance
	// emits the new PROPOSE; for byz leaders we schedule a fabricated one.
	_ = inst.UponRoundTimeout(context.Background(), zap.NewNop())

	newRound := e.round + 1
	newLeader := proposerForRound(s.operators, newRound)
	if s.byz.IsByz(ct.OperatorID(newLeader)) {
		s.scheduleByzProposal(s.now, newLeader, newRound)
	}
	return nil
}

func (s *sim) recordDecided(op spectypes.OperatorID, round specqbft.Round, value []byte) {
	if _, already := s.decided[op]; already {
		return
	}
	// Post-consensus partial-sig collection margin = PhaseBudget (= 2·BTT
	// at defaults). Matches the OBFT family's Δ_2 = 2·BTT convention and
	// the per-phase budget assumption used elsewhere in the QBFT adapter.
	s.decided[op] = decidedRecord{
		value: append([]byte(nil), value...),
		round: round,
		at:    s.now + s.cfg.PhaseBudget,
	}
	// SSV's QBFT-then-post-consensus model: every honest operator that
	// reaches Decided broadcasts one PartialSignatureMessage on the
	// decided value so the cluster can aggregate the full validator
	// signature. Count the bandwidth here (the +1 BTT above already
	// accounts for the time cost); the partial-sig propagation latency
	// is not modeled beyond the BTT-aggregate signal since this phase
	// is past the consensus decision and not load-bearing for the
	// simulator's success/miss accounting.
	if s.cfg.Bandwidth != nil {
		from := ct.OperatorID(op)
		for _, peer := range s.operators {
			to := ct.OperatorID(peer)
			if to == from {
				continue
			}
			s.cfg.Bandwidth.Emission(from, to, ct.KindPostConsensus, -1, postConsensusInnerBytes)
		}
	}
}

// ---- evtByzProposal: byz round-leader fabricates PROPOSE ----------------

type evtByzProposal struct {
	leader spectypes.OperatorID
	round  specqbft.Round
}

func (e *evtByzProposal) describe() string {
	return fmt.Sprintf("ByzProposal[leader=%d round=%d]", e.leader, e.round)
}

func (e *evtByzProposal) handle(s *sim) []scheduledEvent {
	plans := s.byz.ProposalPlanForRound(s, ct.OperatorID(e.leader), int(e.round), s.canonValueForRound(e.round))
	leaderKey := s.keys.OperatorKeys[e.leader]
	from := ct.OperatorID(e.leader)
	frameworkRound := frameworkRoundFor(int(e.round))
	for _, p := range plans {
		msg, err := makeProposalEnvelope(e.leader, leaderKey, s.identifier, specqbft.FirstHeight, e.round, p.V)
		if err != nil {
			continue
		}
		// Wire-byte size charged to bandwidth per-recipient — same accounting
		// path as virtualNetwork.Broadcast for honest PROPOSEs, so byz-side
		// fabricated messages aren't a hidden gap in the bandwidth report.
		msgBytes, encErr := messageWireBytes(msg)
		if encErr != nil && s.cfg.TraceEnabled {
			s.trace = append(s.trace, ct.TraceEntry{
				When:  s.now,
				Event: fmt.Sprintf("ByzProposalEncode-FAILED[leader=%d round=%d err=%v]", e.leader, e.round, encErr),
			})
		}
		recipients := p.Recipients
		if len(recipients) == 0 {
			recipients = make([]ct.OperatorID, 0, len(s.operators))
			for _, op := range s.operators {
				recipients = append(recipients, ct.OperatorID(op))
			}
		}
		for _, to := range recipients {
			toID := spectypes.OperatorID(to)
			if toID == e.leader {
				continue
			}
			delay := s.byz.OverrideDelay(s.rng, from, to, ct.KindLeaderBroadcast)
			if delay < 0 {
				delay = s.cfg.Network.Delay(s.rng, from, to, ct.KindLeaderBroadcast)
			}
			if s.cfg.Bandwidth != nil && msgBytes > 0 {
				s.cfg.Bandwidth.Emission(from, to, ct.KindLeaderBroadcast, frameworkRound, msgBytes)
			}
			s.schedule(s.now+delay, &evtMessageArrival{
				from: from,
				to:   to,
				msg:  msg.DeepCopy(),
			})
		}
	}
	return nil
}
