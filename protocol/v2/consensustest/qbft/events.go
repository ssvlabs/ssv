package qbft

import (
	"context"
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/desim"
	qbftinstance "github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
)

// queueCapacity bounds each operator's virtual inbound queue, mirroring SSV's
// defaultValidatorQueueSize (validator/opts.go). When the queue is full the
// arriving message is dropped — the same TryPush-failure behavior as the
// production queue consumer, so message floods under instability still shed
// load rather than buffering without bound.
const queueCapacity = 128

// pendingMsg is one buffered inbound message in an operator's virtual inbound
// queue (drainQueue). The raw envelope is retained (re-decoded per attempt)
// because a message rejected with a retryable error must be replayable later.
type pendingMsg struct {
	from ct.OperatorID
	msg  *spectypes.SignedSSVMessage
}

// QBFT events run on the shared desim.Engine. Each event's Handle returns
// follow-on desim.Scheduled items; the equal-time round-timeout
// deprioritization that lets the pristine RT = 3·BTT floor hold is installed in
// newSim via Engine.SetEqualTimeLowPriority.

// ---- evtStartInstance: triggers Instance.Start for one honest op ---------

type evtStartInstance struct {
	op spectypes.OperatorID
}

func (e *evtStartInstance) Describe() string {
	return fmt.Sprintf("StartInstance[op=%d]", e.op)
}

func (e *evtStartInstance) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
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

func (e *evtMessageArrival) Describe() string {
	return fmt.Sprintf("MessageArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtMessageArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	op := spectypes.OperatorID(e.to)
	inst := s.instances[op]
	if inst == nil || !inst.CanProcessMessages() {
		return nil
	}
	// Model SSV's per-validator inbound queue (validator/queue_validator.go):
	// enqueue the arrival, then process everything currently processable. A
	// vote that races ahead of its round's proposal is held by drainQueue
	// (retryable ErrNoProposalForCurrentRound) and replayed once the proposal
	// lands, rather than dropped — which is what production's queue (its
	// proposal-gating filter + 25ms retry) does. A full queue drops the new
	// message, matching the production TryPush bound.
	if len(s.pending[op]) >= queueCapacity {
		s.Tracef("QueueFull-drop[from=%d to=%d]", e.from, e.to)
		return nil
	}
	s.pending[op] = append(s.pending[op], pendingMsg{from: e.from, msg: e.msg})
	s.drainQueue(op)
	return nil
}

// drainQueue processes op's buffered inbound messages, looping until no further
// progress is possible. Each pass feeds every buffered message to ProcessMsg:
// applied and non-retryably-rejected messages are removed; messages rejected
// with a retryable error (a vote whose proposal hasn't been accepted yet, or a
// message for a round the instance hasn't reached) are kept for a later drain.
// Applying a proposal — or advancing a round — unblocks held votes, so the loop
// re-runs after any progress and the just-unblocked messages are picked up at
// the same virtual instant. This mirrors the production queue consumer popping
// the proposal first, then the previously-gated votes (validator/queue_validator.go).
func (s *sim) drainQueue(op spectypes.OperatorID) {
	inst := s.instances[op]
	if inst == nil {
		s.pending[op] = nil
		return
	}
	for {
		if !inst.CanProcessMessages() {
			// Instance stopped processing: round-exhausted (reached CutOffRound)
			// or marked irrelevant. NB an ordinary decide does NOT trip this —
			// MarkDecided leaves CanProcessMessages true, so post-decide drains
			// fall through and process/drop normally. Here every remaining
			// message would reject non-retryably, so discard the queue rather
			// than drain it one by one.
			s.pending[op] = nil
			return
		}
		q := s.pending[op]
		if len(q) == 0 {
			return
		}
		progressed := false
		keep := make([]pendingMsg, 0, len(q))
		for _, item := range q {
			if !inst.CanProcessMessages() {
				// Stopped processing mid-pass (e.g. a round-change bumped the
				// round to CutOffRound); the outer loop discards the remainder.
				break
			}
			pm, err := specqbft.NewProcessingMessage(item.msg)
			if err != nil {
				s.Tracef("MessageDecode-FAILED[from=%d to=%d err=%v]", item.from, op, err)
				progressed = true
				continue
			}
			// Stash the in-flight round so virtualValueChecker.CheckValue (called
			// inside ProcessMsg → uponProposal → isProposalJustification) reports
			// the proposal's round to the host instead of Instance.State.Round
			// (not yet bumped). Cleared after ProcessMsg returns.
			s.inflightRound[op] = pm.QBFTMessage.Round
			decided, decidedValue, _, perr := inst.ProcessMsg(context.Background(), zap.NewNop(), pm)
			delete(s.inflightRound, op)
			if perr != nil {
				if qbftinstance.IsRetryable(perr) {
					keep = append(keep, item) // hold for a later drain
					continue
				}
				s.Tracef("ProcessMsg-rejected[from=%d to=%d type=%d round=%d err=%v]",
					item.from, op, pm.QBFTMessage.MsgType, pm.QBFTMessage.Round, perr)
				progressed = true
				continue
			}
			progressed = true
			if decided {
				s.recordDecided(op, pm.QBFTMessage.Round, decidedValue)
			}
		}
		s.pending[op] = keep
		if !progressed {
			return // only retryable-held messages remain; nothing more to do now
		}
	}
}

// ---- evtRoundTimeout: virtual round timer fires --------------------------

type evtRoundTimeout struct {
	op    spectypes.OperatorID
	round specqbft.Round
	mySeq int64
}

func (e *evtRoundTimeout) Describe() string {
	return fmt.Sprintf("RoundTimeout[op=%d round=%d]", e.op, e.round)
}

func (e *evtRoundTimeout) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
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

	// Advancing the round may unblock buffered messages that were waiting for
	// it (held as retryable by drainQueue while the instance was a round behind).
	s.drainQueue(e.op)

	newRound := e.round + 1
	newLeader := proposerForRound(s.operators, newRound)
	if s.byz.IsByz(ct.OperatorID(newLeader)) {
		s.scheduleByzProposal(s.Now(), newLeader, newRound)
	}
	return nil
}

func (s *sim) recordDecided(op spectypes.OperatorID, round specqbft.Round, value []byte) {
	if _, already := s.decided[op]; already {
		return
	}
	// Record the local QBFT-consensus decision (value + round). The
	// "ready to submit" time that Outcome.DecisionTime exposes is tracked
	// separately, accruing as the op's partials-on-value count reaches
	// 2f+1 — see outcome() for the earliest-ready derivation.
	s.decided[op] = decidedRecord{
		value: append([]byte(nil), value...),
		round: round,
	}
	// SSV's QBFT-then-post-consensus model: every honest operator that
	// reaches Decided broadcasts one PartialSignatureMessage on the
	// decided value so the cluster can aggregate the full validator
	// signature. The sim models this as real events through the same
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
	// The decided value is immutable, and the only consumer (recordPartialSig)
	// reads it solely to key s.partials by string(value) — it never retains or
	// mutates the slice. So every per-recipient arrival can share one
	// append-safe backing (3-index slice → cap == len, so any append
	// reallocates) instead of each copying ~5 KB. Mirrors the obft/twoab
	// adapters' shareImmutable fan-out sharing.
	shared := value[:len(value):len(value)]
	build := func(to ct.OperatorID) desim.Event {
		return &evtPartialSigArrival{
			from:  from,
			to:    to,
			value: shared,
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
		delay := s.byz.OverrideDelay(s.Rng(), from, to, ct.KindPostConsensus)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.Rng(), from, to, ct.KindPostConsensus)
		}
		if s.cfg.Bandwidth != nil {
			s.cfg.Bandwidth.Emission(from, to, ct.KindPostConsensus, -1, postConsensusInnerBytes)
		}
		s.Schedule(s.Now()+delay, &evtPartialSigArrival{
			from:  from,
			to:    to,
			value: shared,
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

func (e *evtPartialSigArrival) Describe() string {
	return fmt.Sprintf("PartialSigArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtPartialSigArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	s.recordPartialSig(spectypes.OperatorID(e.to), spectypes.OperatorID(e.from), e.value)
	return nil
}

// ---- evtByzProposal: byz round-leader fabricates PROPOSE ----------------

type evtByzProposal struct {
	leader spectypes.OperatorID
	round  specqbft.Round
}

func (e *evtByzProposal) Describe() string {
	return fmt.Sprintf("ByzProposal[leader=%d round=%d]", e.leader, e.round)
}

func (e *evtByzProposal) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	plans := s.byz.ProposalPlanForRound(s, ct.OperatorID(e.leader), int(e.round), s.canonValueForRound(e.round))
	// Equivocation observation: count distinct V's the byz leader emits
	// at this (leader, round). A 1-plan call adds zero; a 2-V split adds
	// one; a 1-1-1 split adds two. scheduleByzProposal already dedups
	// one evtByzProposal per round (see des.go scheduleByzProposal), so
	// the sum across rounds matches "total equivocation events emitted
	// in this sim".
	if len(plans) > 1 {
		distinctV := make(map[string]struct{}, len(plans))
		for _, p := range plans {
			distinctV[string(p.V)] = struct{}{}
		}
		if len(distinctV) > 1 {
			s.equivocationsObserved += len(distinctV) - 1
		}
	}
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
		if encErr != nil {
			s.Tracef("ByzProposalEncode-FAILED[leader=%d round=%d err=%v]", e.leader, e.round, encErr)
		}
		recipients := p.Recipients
		if len(recipients) == 0 {
			recipients = make([]ct.OperatorID, 0, len(s.operators))
			for _, op := range s.operators {
				recipients = append(recipients, ct.OperatorID(op))
			}
		}
		msgCap := msg
		build := func(to ct.OperatorID) desim.Event {
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
			delay := s.byz.OverrideDelay(s.Rng(), from, to, ct.KindLeaderBroadcast)
			if delay < 0 {
				delay = s.cfg.Network.Delay(s.Rng(), from, to, ct.KindLeaderBroadcast)
			}
			if s.cfg.Bandwidth != nil && msgBytes > 0 {
				s.cfg.Bandwidth.Emission(from, to, ct.KindLeaderBroadcast, frameworkRound, msgBytes)
			}
			s.Schedule(s.Now()+delay, &evtMessageArrival{
				from: from,
				to:   to,
				msg:  msg.DeepCopy(),
			})
		}
	}
	return nil
}
