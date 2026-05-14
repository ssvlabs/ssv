package twoab

import (
	"container/heap"
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// event is the discrete-event simulator's unit of work.
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

// ---- evtMeshArrival ----------------------------------------------------

// evtMeshArrival mirrors the OBFT adapter's mesh delivery event. See
// protocol/v2/consensustest/obft/events.go evtMeshArrival for the design
// rationale (dedup → optional local-protocol delivery → forward to
// neighbors). 2abOBFT shares the wiring shape; only the per-protocol
// builder closure differs.
type evtMeshArrival struct {
	from, to ct.MeshNode
	msgID    ct.MsgID
	kind     ct.MsgKind
	layer    int
	bytes    int64
	builder  func(to twoab.OperatorID) event
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
		recipientOp := twoab.OperatorID(mesh.OperatorForNode(e.to))
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
		mesh.RecordMeshHop(s.cfg.Bandwidth, e.to, neighbor, e.kind, e.layer, e.bytes)
		out = append(out, scheduledEvent{
			when: s.now + mesh.ValidateDelay() + delay,
			ev: &evtMeshArrival{
				from:    e.to,
				to:      neighbor,
				msgID:   e.msgID,
				kind:    e.kind,
				layer:   e.layer,
				bytes:   e.bytes,
				builder: e.builder,
			},
		})
	}
	return out
}

// ---- evtLeaderFetch ----------------------------------------------------

type evtLeaderFetch struct {
	layer int
}

func (e *evtLeaderFetch) describe() string { return fmt.Sprintf("LeaderFetch[layer=%d]", e.layer) }

func (e *evtLeaderFetch) handle(s *sim) []scheduledEvent {
	leader := s.cfgTwoab.Layers[e.layer].Leader
	plans := s.cfg.Byz.LeaderBroadcastPlan(s, leader, e.layer, s.honestLeaderValue(e.layer))

	var out []scheduledEvent
	for _, p := range plans {
		bundle, err := s.instances[leader].BuildPhase1Bundle(e.layer, p.V)
		if err != nil {
			// Byz operator forging — synthesize a bundle directly.
			bundle = &twoab.Phase1Bundle{
				ClusterID:  s.cfgTwoab.ClusterID,
				OperatorID: leader,
				Height:     s.cfgTwoab.Height,
				Layer:      e.layer,
				Value:      append(twoab.Value{}, p.V...),
			}
		} else {
			// Leader self-observes their own bundle so their own retention
			// state reflects V at this layer. 2ab Variant C has no Phase-1
			// σ_V partial (and thus no LeaderSigmaWitness array in Phase 2
			// that would rehydrate the leader's σ_V cluster-wide); without
			// self-observation the leader contributes nothing to their own
			// Phase-2a verdict pool, breaking the all-honest healthy path.
			_ = s.instances[leader].ObservePhase1Bundle(bundle, s.observedOffset())
			leaderValid := s.cfg.Host.Validate(ct.OperatorID(leader), e.layer, p.V, ct.PhasePhase1Acceptance)
			_ = s.instances[leader].ApplyHostValidity(e.layer, p.V, leaderValid)
		}
		// 2ab Phase 1 carries no σ_V — no offline-aggregator ObserveSigma at
		// leader-fetch time. σ partials are credited later via Onion2b.
		bundleBytes := phase1BundleSize(bundle)
		ownDelay := s.cfg.Byz.OverrideOwnPhase1Delay(s, leader)
		recipients := p.Recipients
		if recipients == nil {
			recipients = s.operators
		}
		bundleCap := bundle
		layerCap := e.layer
		build := func(to twoab.OperatorID) event {
			return &evtPhase1Arrival{from: leader, to: to, layer: layerCap, bundle: clonePhase1Bundle(bundleCap)}
		}
		if s.cfg.Mesh != nil {
			s.emitMesh(leader, ct.KindLeaderBroadcast, e.layer, bundleBytes, ownDelay, recipients, build)
			continue
		}
		for _, to := range recipients {
			if to == leader {
				continue
			}
			delay := s.cfg.Byz.OverrideDelay(s.rng, leader, to, ct.KindLeaderBroadcast)
			if delay < 0 {
				delay = s.cfg.Network.Delay(s.rng, ct.OperatorID(leader), ct.OperatorID(to), ct.KindLeaderBroadcast)
			}
			if s.cfg.Bandwidth != nil && bundleBytes > 0 {
				s.cfg.Bandwidth.Emission(ct.OperatorID(leader), ct.OperatorID(to),
					ct.KindLeaderBroadcast, e.layer, bundleBytes)
			}
			out = append(out, scheduledEvent{
				when: s.now + delay + ownDelay,
				ev:   &evtPhase1Arrival{from: leader, to: to, layer: e.layer, bundle: clonePhase1Bundle(bundle)},
			})
		}
	}
	return out
}

// ---- evtPhase1Arrival --------------------------------------------------

type evtPhase1Arrival struct {
	from, to twoab.OperatorID
	layer    int
	bundle   *twoab.Phase1Bundle
}

func (e *evtPhase1Arrival) describe() string {
	return fmt.Sprintf("Phase1Arrival[from=%d to=%d layer=%d v=%s]",
		e.from, e.to, e.layer, valuePrefix(e.bundle.Value))
}

func (e *evtPhase1Arrival) handle(s *sim) []scheduledEvent {
	inst := s.instances[e.to]
	if err := inst.ObservePhase1Bundle(e.bundle, s.observedOffset()); err != nil {
		return nil
	}
	valid := s.cfg.Host.Validate(ct.OperatorID(e.to), e.layer, e.bundle.Value, ct.PhasePhase1Acceptance)
	_ = inst.ApplyHostValidity(e.layer, e.bundle.Value, valid)
	return nil
}

// ---- evtVerdictBroadcastStart ------------------------------------------

// evtVerdictBroadcastStart fires at T_verdict_max - ε_proc per spec
// §Phase 2a. Each non-suppressed operator computes their per-layer verdict
// + broadcasts the envelope to all peers; byz patterns can selectively
// suppress verdict emission, override the verdict body, or inject
// additional distinct verdicts (Rule 6a equivocation).
//
// Receivers' verdicts arrive at evtVerdictArrival and feed into the
// receiver's verdict pool for convergence-rule input at Phase 2b sign time.
type evtVerdictBroadcastStart struct{}

func (e *evtVerdictBroadcastStart) describe() string { return "VerdictBroadcastStart" }

func (e *evtVerdictBroadcastStart) handle(s *sim) []scheduledEvent {
	K := s.cfg.K
	for _, op := range s.operators {
		for k := 0; k < K; k++ {
			if !s.cfg.Byz.AllowVerdictBroadcast(op, k) {
				continue
			}
			v, err := s.instances[op].BuildVerdict(k)
			if err != nil || v == nil {
				continue
			}
			v = s.cfg.Byz.OverrideVerdict(s, op, k, v)
			vBytes := verdictSize(v)
			s.emitToAll(op, ct.KindCommit, k, vBytes, 0, func(to twoab.OperatorID) event {
				return &evtVerdictArrival{from: op, to: to, verdict: cloneVerdict(v)}
			})
			// Byz Rule 6a equivocation: emit ADDITIONAL distinct verdicts
			// at the same (op, layer). Each is delivered cluster-wide so
			// every honest receiver sees the equivocation.
			for _, extra := range s.cfg.Byz.BuildExtraVerdicts(s, op, k, v) {
				extra := extra
				exBytes := verdictSize(extra)
				s.emitToAll(op, ct.KindCommit, k, exBytes, 0, func(to twoab.OperatorID) event {
					return &evtVerdictArrival{from: op, to: to, verdict: cloneVerdict(extra)}
				})
			}
		}
	}
	// emitToAll schedules directly via s.schedule; nothing to return.
	return nil
}

// ---- evtVerdictArrival -------------------------------------------------

type evtVerdictArrival struct {
	from, to twoab.OperatorID
	verdict  *twoab.Verdict
}

func (e *evtVerdictArrival) describe() string {
	return fmt.Sprintf("VerdictArrival[from=%d to=%d layer=%d kind=%s]",
		e.from, e.to, e.verdict.Layer, e.verdict.Kind)
}

func (e *evtVerdictArrival) handle(s *sim) []scheduledEvent {
	_ = s.instances[e.to].ObserveVerdict(e.verdict)
	return nil
}

// ---- evtPhaseTwoBStart -------------------------------------------------

// evtPhaseTwoBStart fires at T_commit. Each non-suppressed operator
// builds their Onion2b and broadcasts; byz patterns can suppress,
// override, or inject extra Onion2b messages (Rule 3 top-level cross-onion
// equivocation).
type evtPhaseTwoBStart struct{}

func (e *evtPhaseTwoBStart) describe() string { return "PhaseTwoBStart" }

func (e *evtPhaseTwoBStart) handle(s *sim) []scheduledEvent {
	for _, op := range s.operators {
		if !s.cfg.Byz.AllowOnion2bBroadcast(op) {
			continue
		}
		o, err := s.instances[op].BuildOwnOnion2b()
		if err != nil || o == nil {
			continue
		}
		o = s.cfg.Byz.OverrideOnion2b(s, op, o)
		// OfflineAggregator: record per-layer σ / NR / encrypted-claim partials.
		if s.cfg.Aggregator != nil {
			recordOnion2bToAggregator(s.cfg.Aggregator, o)
		}
		extraDelay := s.cfg.Byz.OverrideOwnOnion2bDispatchDelay(s, op)
		s.emitToAll(op, ct.KindCommit, -1, onion2bSize(o), extraDelay, func(to twoab.OperatorID) event {
			return &evtOnion2bArrival{from: op, to: to, onion: cloneOnion2b(o)}
		})
		for _, extra := range s.cfg.Byz.BuildExtraOnion2bs(s, op, o) {
			extra := extra
			if s.cfg.Aggregator != nil {
				recordOnion2bToAggregator(s.cfg.Aggregator, extra)
			}
			s.emitToAll(op, ct.KindCommit, -1, onion2bSize(extra), extraDelay, func(to twoab.OperatorID) event {
				return &evtOnion2bArrival{from: op, to: to, onion: cloneOnion2b(extra)}
			})
		}
		// Observer-mode self-resolve probe: BuildOwnOnion2b self-observes
		// the op's own σ/NR partials into its local pools, so an op at
		// f=0 (qV=1) can satisfy σ-quorum from its own self-observation
		// alone. At realistic f≥1 the probe returns ErrNoQuorum (1 partial
		// < qV) and is a no-op. Mirrors OBFT's evtPhaseTwoStart probe per
		// OBFT-OPPORTUNISTIC-PHASE3-PLAN.md §2abOBFT instrumentation table.
		tryOpportunisticResolve(s, op)
	}
	return nil
}

// recordOnion2bToAggregator extracts every σ / NR / encrypted-onion partial
// from o and records it in the aggregator. Mirrors base's
// recordCommitToAggregator: credits o.OperatorID (the claimed sender on
// the wire), NOT the actual emitter. The 2ab adapter has no Witnesses
// array (no Phase-1 σ_V exists), so this is simpler than the base
// adapter's equivalent.
//
// At L_0 the EncryptedLayer.Ciphertext holds the plaintext σ partial bytes
// directly (no IBE wrapping); deeper layers carry chained-IBE ciphertext.
func recordOnion2bToAggregator(agg *ct.OfflineAggregator, o *twoab.Onion2b) {
	from := ct.OperatorID(o.OperatorID)
	for layer, el := range o.Layers {
		if len(el.Value) == 0 {
			continue
		}
		if layer == 0 {
			agg.ObserveSigma(from, layer, el.Value)
		} else {
			agg.ObserveEncryptedClaim(from, layer, el.Value)
		}
	}
	for _, nr := range o.NRPartials {
		agg.ObserveNR(from, nr.Layer)
	}
}

// ---- evtOnion2bArrival -------------------------------------------------

type evtOnion2bArrival struct {
	from, to twoab.OperatorID
	onion    *twoab.Onion2b
}

func (e *evtOnion2bArrival) describe() string {
	return fmt.Sprintf("Onion2bArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtOnion2bArrival) handle(s *sim) []scheduledEvent {
	_ = s.instances[e.to].ObserveOnion2b(e.onion)

	// Observer-mode quorum detection: probe Resolve immediately on every
	// onion arrival. First-success records vQuorumAt[e.to] = s.now —
	// the earliest moment this op holds a submittable σ-cert. Resolve
	// is stateless / idempotent (spec §Phase 3) so probing on every
	// arrival is safe. The Outcome layer reads vQuorumAt as the
	// BTT-sensitive DecisionTime. See docs/OBFT-OPPORTUNISTIC-PHASE3-PLAN.md.
	tryOpportunisticResolve(s, e.to)

	// Spec §Phase 3 "Re-running on late KindOnion2b arrivals": if this
	// onion landed past RoundEndOffset, the receiver re-runs Resolve to
	// incorporate the new partial. Skip if the receiver already decided.
	if s.now <= s.cfgTwoab.RoundEndOffset() {
		return nil
	}
	if _, already := s.resolved[e.to]; already {
		return nil
	}
	return []scheduledEvent{{when: s.now, ev: &evtResolveRerun{op: e.to}}}
}

// tryOpportunisticResolve mirrors the OBFT adapter's helper of the
// same name. See protocol/v2/consensustest/obft/events.go for the
// design rationale. 2abOBFT differs only in the trigger event
// (evtOnion2bArrival here vs evtCommitArrival in base) and the
// concrete Resolve impl walks the chained-NR ladder via Onion2b
// contributions instead of Commit. The dedup-and-record step delegates
// to the framework-level RecordFirstOpportunisticQuorum helper.
func tryOpportunisticResolve(s *sim, op twoab.OperatorID) {
	if _, already := s.vQuorumAt[op]; already {
		return
	}
	res, err := s.instances[op].Resolve()
	if err != nil {
		return
	}
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, op, res.Layer, s.now, s.cfg.Epsilon3)
}

// ---- evtResolve --------------------------------------------------------

type evtResolve struct{}

func (e *evtResolve) describe() string { return "Resolve" }

func (e *evtResolve) handle(s *sim) []scheduledEvent {
	var out []scheduledEvent
	for _, op := range s.operators {
		out = append(out, resolveOpAndBroadcastCert(s, op)...)
	}
	return out
}

// ---- evtResolveRerun ---------------------------------------------------

type evtResolveRerun struct {
	op twoab.OperatorID
}

func (e *evtResolveRerun) describe() string {
	return fmt.Sprintf("ResolveRerun[op=%d]", e.op)
}

func (e *evtResolveRerun) handle(s *sim) []scheduledEvent {
	if _, decided := s.resolved[e.op]; decided {
		return nil
	}
	return resolveOpAndBroadcastCert(s, e.op)
}

// resolveOpAndBroadcastCert runs Resolve for `op`, records the outcome in
// the sim's resolved/resolveErrs maps, and (on success) schedules cert
// broadcast to peers.
//
// On success, clears any prior resolveErrs entry so the late-recovered op
// reports decided=true with no Err in the outcome.
func resolveOpAndBroadcastCert(s *sim, op twoab.OperatorID) []scheduledEvent {
	res, err := s.instances[op].Resolve()
	if err != nil {
		if _, decided := s.resolved[op]; !decided {
			s.resolveErrs[op] = err
		}
		return nil
	}
	s.resolved[op] = res
	// Phase-3 walk cost grows linearly with the number of layers visited.
	// Mirror the OBFT adapter's per-layer ε_3 accrual.
	decisionTime := s.now + time.Duration(res.Layer)*s.cfg.Epsilon3
	s.resolvedAt[op] = decisionTime
	// Schedule-anchored fallback for the observer-mode metric: if no
	// prior onion/cert arrival already set vQuorumAt, capture the
	// schedule-anchored decision time here so outcome() reads
	// uniformly from vQuorumAt.
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, op, res.Layer, s.now, s.cfg.Epsilon3)
	delete(s.resolveErrs, op)

	if !s.cfg.Byz.AllowCertificateBroadcast(op) {
		return nil
	}
	cert, err := s.instances[op].BuildCertificate(res)
	if err != nil || cert == nil {
		return nil
	}
	certBytes := certSize(cert)
	certCap := cert
	opCap := op
	build := func(to twoab.OperatorID) event {
		return &evtCertArrival{from: opCap, to: to, cert: cloneCertificate(certCap)}
	}
	extraDelay := decisionTime - s.now
	if s.cfg.Mesh != nil {
		s.emitMesh(op, ct.KindCertificate, -1, certBytes, extraDelay, s.operators, build)
		return nil
	}
	var out []scheduledEvent
	for _, to := range s.operators {
		if to == op {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(op, to, ct.KindCertificate) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.rng, op, to, ct.KindCertificate)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, ct.OperatorID(op), ct.OperatorID(to), ct.KindCertificate)
		}
		if s.cfg.Bandwidth != nil && certBytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(op), ct.OperatorID(to),
				ct.KindCertificate, -1, certBytes)
		}
		out = append(out, scheduledEvent{
			when: decisionTime + delay,
			ev:   &evtCertArrival{from: op, to: to, cert: cloneCertificate(cert)},
		})
	}
	return out
}

// ---- evtCertArrival ----------------------------------------------------

type evtCertArrival struct {
	from, to twoab.OperatorID
	cert     *twoab.Certificate
}

func (e *evtCertArrival) describe() string {
	return fmt.Sprintf("CertArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtCertArrival) handle(s *sim) []scheduledEvent {
	inst := s.instances[e.to]
	if err := inst.ObserveCertificate(e.cert); err != nil {
		return nil
	}
	if _, already := s.resolved[e.to]; already {
		return nil
	}
	s.resolved[e.to] = &twoab.Output{
		Layer:     -1,
		Value:     append(twoab.Value{}, e.cert.Value...),
		Signature: append(twoab.Signature{}, e.cert.Signature...),
	}
	s.resolvedAt[e.to] = s.now
	// Cert receipt is itself the earliest "submittable" moment for this
	// op — mirror OBFT's evtCertArrival. Layer is unknown for cert-decided
	// outputs; pass 0 so the recorded time is exactly s.now (no walk
	// cost).
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, e.to, 0, s.now, s.cfg.Epsilon3)
	delete(s.resolveErrs, e.to)
	return nil
}
