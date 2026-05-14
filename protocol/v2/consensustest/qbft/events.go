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

// ---- evtMeshArrival: per-hop delivery in DeliveryMesh mode ---------------

// evtMeshArrival mirrors the OBFT and 2abOBFT adapters' mesh delivery
// event. See protocol/v2/consensustest/obft/events.go evtMeshArrival for
// the design rationale. QBFT-specific note: the builder constructs an
// evtMessageArrival; the self-loopback path (zero-delay delivery to the
// publisher's own container) is handled in Broadcast directly and does
// not flow through mesh.
type evtMeshArrival struct {
	from, to ct.MeshNode
	msgID    ct.MsgID
	kind     ct.MsgKind
	round    int
	bytes    int64
	builder  func(to ct.OperatorID) event
}

func (e *evtMeshArrival) describe() string {
	return fmt.Sprintf("MeshArrival[from=%d to=%d msg=%d kind=%s]",
		e.from, e.to, e.msgID, e.kind)
}

func (e *evtMeshArrival) handle(s *sim) []scheduledEvent {
	mesh := s.cfg.Mesh
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
		if neighbor == e.from {
			continue
		}
		delay := mesh.SampleHopDelay(s.rng, fromEP, mesh.EndpointFor(neighbor), e.kind)
		mesh.RecordMeshHop(s.cfg.Bandwidth, e.to, neighbor, e.kind, e.round, e.bytes)
		out = append(out, scheduledEvent{
			when: s.now + mesh.ValidateDelay() + delay,
			ev: &evtMeshArrival{
				from:    e.to,
				to:      neighbor,
				msgID:   e.msgID,
				kind:    e.kind,
				round:   e.round,
				bytes:   e.bytes,
				builder: e.builder,
			},
		})
	}
	return out
}

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
	// Local QBFT-consensus decision: stamped at s.now (no PhaseBudget
	// shift). The "ready to submit" time — which is what
	// Outcome.DecisionTime now exposes — accrues separately as op's
	// partials-on-value count reaches 2f+1. See outcome() for the
	// earliest-ready derivation.
	s.decided[op] = decidedRecord{
		value: append([]byte(nil), value...),
		round: round,
		at:    s.now,
	}
	// SSV's QBFT-then-post-consensus model: every honest operator that
	// reaches Decided broadcasts one PartialSignatureMessage on the
	// decided value so the cluster can aggregate the full validator
	// signature. Phase C models this as real events through the same
	// broadcast transport (mesh or direct per cfg.Delivery); 2f+1
	// distinct partials at any receiver mark that receiver as "ready
	// to submit" via recordPartialSig.
	//
	// Self-record: the deciding operator's own partial sig is available
	// locally without going through the network (analogous to QBFT's
	// loopback self-delivery of consensus messages).
	s.recordPartialSig(op, op, value)
	s.broadcastPartialSig(ct.OperatorID(op), value)
}

// broadcastPartialSig emits the deciding op's PartialSignatureMessage
// to every other operator via the configured transport. Direct mode
// fanout mirrors virtualNetwork.Broadcast (per-pair delay + byz
// AllowDelivery / OverrideDelay primitives); mesh mode routes through
// emitMesh. Self-loopback is handled by the caller (recordPartialSig
// for the op's own partial).
func (s *sim) broadcastPartialSig(from ct.OperatorID, value []byte) {
	valueBytes := append([]byte(nil), value...)
	build := func(to ct.OperatorID) event {
		return &evtPartialSigArrival{
			from:  from,
			to:    to,
			value: append([]byte(nil), valueBytes...),
		}
	}
	if s.cfg.Mesh != nil {
		s.emitMesh(from, ct.KindPostConsensus, -1, postConsensusInnerBytes, 0, s.operatorsCT(), build)
		return
	}
	for _, peer := range s.operators {
		to := ct.OperatorID(peer)
		if to == from {
			continue
		}
		if !s.byz.AllowDelivery(from, to, ct.KindPostConsensus) {
			continue
		}
		delay := s.byz.OverrideDelay(s.rng, from, to, ct.KindPostConsensus)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, from, to, ct.KindPostConsensus)
		}
		if s.cfg.Bandwidth != nil {
			s.cfg.Bandwidth.Emission(from, to, ct.KindPostConsensus, -1, postConsensusInnerBytes)
		}
		s.schedule(s.now+delay, &evtPartialSigArrival{
			from:  from,
			to:    to,
			value: append([]byte(nil), valueBytes...),
		})
	}
}

// ---- evtPartialSigArrival: KindPostConsensus delivery ------------------

// evtPartialSigArrival models the receive-side of one
// PartialSignatureMessage. The handler records the (signer, value)
// observation in the receiver's partials state; if that observation
// pushes the receiver's distinct-signer count to 2f+1, the receiver's
// readyAt is stamped and contributes to Outcome.DecisionTime.
type evtPartialSigArrival struct {
	from, to ct.OperatorID
	value    []byte
}

func (e *evtPartialSigArrival) describe() string {
	return fmt.Sprintf("PartialSigArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtPartialSigArrival) handle(s *sim) []scheduledEvent {
	s.recordPartialSig(spectypes.OperatorID(e.to), spectypes.OperatorID(e.from), e.value)
	return nil
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
		msgCap := msg
		build := func(to ct.OperatorID) event {
			return &evtMessageArrival{
				from: from,
				to:   to,
				msg:  msgCap.DeepCopy(),
			}
		}
		if s.cfg.Mesh != nil {
			// Route byz proposals through the mesh too when delivery is
			// mesh-mode (no catalog scenario combines byz + mesh today,
			// but routing here keeps the transport choice consistent
			// with virtualNetwork.Broadcast and the honest path).
			s.emitMesh(from, ct.KindLeaderBroadcast, frameworkRound, msgBytes, 0, recipients, build)
			continue
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
