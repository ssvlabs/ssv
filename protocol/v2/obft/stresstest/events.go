package stresstest

import (
	"container/heap"
	"fmt"
	"time"

	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
)

// event is the discrete-event simulator's unit of work. Handlers run on
// the single event-loop goroutine, get the sim mutated state, and return
// any new events to schedule.
type event interface {
	handle(s *sim) []scheduledEvent
	describe() string
}

// scheduledEvent is the (time, event) pair returned from a handler so the
// loop can re-enqueue under the heap's ordering.
type scheduledEvent struct {
	when time.Duration
	ev   event
}

// queueItem is the heap node. Sequence number breaks ties on equal
// timestamps to keep the dispatch order deterministic.
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

// Compile-time assertion that eventQueue satisfies heap.Interface.
var _ heap.Interface = (*eventQueue)(nil)

// ---- evtLeaderFetch -----------------------------------------------------

// evtLeaderFetch fires at the layer's FetchAt. The byz pattern decides
// whether this leader actually broadcasts (and what; equivocation results
// in two scheduled bundles to disjoint receiver sets).
type evtLeaderFetch struct {
	layer int
}

func (e *evtLeaderFetch) describe() string { return fmt.Sprintf("LeaderFetch[layer=%d]", e.layer) }

func (e *evtLeaderFetch) handle(s *sim) []scheduledEvent {
	leader := s.cfgObft.Layers[e.layer].Leader
	plans := s.cfg.Byz.LeaderBroadcastPlan(s, leader, e.layer, s.honestLeaderValue(e.layer))

	var out []scheduledEvent
	for _, p := range plans {
		bundle, err := s.instances[leader].BuildPhase1Bundle(e.layer, p.V)
		if err != nil {
			// Leader's own EKM rejected the second V (single-σ-V invariant).
			// Byz simulation needs a side-channel; build via a fresh signer.
			bundle = s.forgeByzBundle(leader, e.layer, p.V)
		} else {
			// Leader's instance self-observed via BuildPhase1Bundle. Apply
			// host validity at the leader for their own V.
			_ = s.instances[leader].ApplyHostValidity(e.layer, p.V, true)
		}
		// Schedule arrivals at the operators in p.Recipients (or all peers
		// if Recipients is nil).
		recipients := p.Recipients
		if recipients == nil {
			recipients = s.operators
		}
		for _, to := range recipients {
			if to == leader {
				continue
			}
			delay := s.cfg.Byz.OverrideDelay(s.rng, leader, to, KindPhase1Bundle)
			if delay < 0 {
				delay = s.cfg.Network.Delay(s.rng, leader, to, KindPhase1Bundle)
			}
			out = append(out, scheduledEvent{
				when: s.now + delay,
				ev:   &evtPhase1Arrival{from: leader, to: to, layer: e.layer, bundle: bundle},
			})
		}
	}
	return out
}

// forgeByzBundle constructs a Phase-1 bundle directly via the operator's
// share-bound signer, sidestepping the leader's own EKM lock — used for
// byz scenarios where the leader signs two different V's at the same
// (slot, layer). Picks stub or real-BLS signer to match the sim's crypto
// mode.
func (s *sim) forgeByzBundle(leader obft.OperatorID, layer int, v obft.Value) *obft.Phase1Bundle {
	var signer obft.Signer
	if s.cfg.BLSKeys != nil {
		signer = blsbackend.New(s.cfg.BLSKeys.Shares[leader])
	} else {
		q := s.cfgObft.QV()
		signer = obft.NewStubSigner(q, []byte{byte(leader)})
	}
	sig, err := signer.SignPartial(v)
	if err != nil {
		panic(fmt.Sprintf("stresstest: forge bundle for leader %d: %v", leader, err))
	}
	return &obft.Phase1Bundle{
		OperatorID: leader,
		Height:     s.cfgObft.Height,
		Layer:      layer,
		Value:      append(obft.Value{}, v...),
		SigmaV:     sig,
	}
}

// ---- evtPhase1Arrival ---------------------------------------------------

type evtPhase1Arrival struct {
	from, to obft.OperatorID
	layer    int
	bundle   *obft.Phase1Bundle
}

func (e *evtPhase1Arrival) describe() string {
	return fmt.Sprintf("Phase1Arrival[from=%d to=%d layer=%d v=%s]", e.from, e.to, e.layer, hashValue(e.bundle.Value))
}

func (e *evtPhase1Arrival) handle(s *sim) []scheduledEvent {
	inst := s.instances[e.to]
	if err := inst.ObservePhase1Bundle(e.bundle, s.observedOffset()); err != nil {
		// Late-bundle / auth failure / etc. — protocol-level expected error
		// in some scenarios, just record and move on.
		return nil
	}
	valid := s.cfg.Host.Validate(e.to, e.layer, e.bundle.Value)
	_ = inst.ApplyHostValidity(e.layer, e.bundle.Value, valid)
	return nil
}

// ---- evtPhaseTwoStart ---------------------------------------------------

type evtPhaseTwoStart struct{}

func (e *evtPhaseTwoStart) describe() string { return "PhaseTwoStart" }

func (e *evtPhaseTwoStart) handle(s *sim) []scheduledEvent {
	for _, op := range s.operators {
		if !s.cfg.Byz.AllowCommitBroadcast(op) {
			continue
		}
		c, err := s.instances[op].BuildOwnCommit()
		if err != nil || c == nil {
			continue
		}
		// Apply byz-pattern overrides on the Commit contents (e.g., faked
		// plaintext σ at L_0 for fake-σ scenarios).
		c = s.cfg.Byz.OverrideCommit(s, op, c)
		s.emitToAll(op, KindCommit, func(to obft.OperatorID) event {
			return &evtCommitArrival{from: op, to: to, commit: c}
		})
	}
	return nil
}

// ---- evtCommitArrival --------------------------------------------------

type evtCommitArrival struct {
	from, to obft.OperatorID
	commit   *obft.Commit
}

func (e *evtCommitArrival) describe() string {
	return fmt.Sprintf("CommitArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtCommitArrival) handle(s *sim) []scheduledEvent {
	_ = s.instances[e.to].ObserveCommit(e.commit)
	return nil
}

// ---- evtResolve --------------------------------------------------------

type evtResolve struct{}

func (e *evtResolve) describe() string { return "Resolve" }

func (e *evtResolve) handle(s *sim) []scheduledEvent {
	var out []scheduledEvent
	for _, op := range s.operators {
		res, err := s.instances[op].Resolve()
		if err != nil {
			s.resolveErrs[op] = err
			continue
		}
		s.resolved[op] = res
		s.resolvedAt[op] = s.now
		// On success, schedule a Certificate broadcast (modeling the
		// final-certificate gossip path).
		if !s.cfg.Byz.AllowCertificateBroadcast(op) {
			continue
		}
		cert, err := s.instances[op].BuildCertificate(res)
		if err != nil || cert == nil {
			continue
		}
		for _, to := range s.operators {
			if to == op {
				continue
			}
			if !s.cfg.Byz.AllowDelivery(op, to, KindCertificate) {
				continue
			}
			delay := s.cfg.Byz.OverrideDelay(s.rng, op, to, KindCertificate)
			if delay < 0 {
				delay = s.cfg.Network.Delay(s.rng, op, to, KindCertificate)
			}
			out = append(out, scheduledEvent{
				when: s.now + delay,
				ev:   &evtCertArrival{from: op, to: to, cert: cert},
			})
		}
	}
	return out
}

// ---- evtCertArrival ----------------------------------------------------

type evtCertArrival struct {
	from, to obft.OperatorID
	cert     *obft.Certificate
}

func (e *evtCertArrival) describe() string {
	return fmt.Sprintf("CertArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtCertArrival) handle(s *sim) []scheduledEvent {
	inst := s.instances[e.to]
	if err := inst.ObserveCertificate(e.cert); err != nil {
		return nil
	}
	// If `to` hasn't decided yet locally, the certificate is now their
	// fallback submission path — record as if they decided.
	if _, already := s.resolved[e.to]; already {
		return nil
	}
	s.resolved[e.to] = &obft.Output{
		Layer:     -1, // unknown layer; cert path doesn't carry it
		Value:     append(obft.Value{}, e.cert.Value...),
		Signature: append(obft.Signature{}, e.cert.Signature...),
	}
	s.resolvedAt[e.to] = s.now
	return nil
}
