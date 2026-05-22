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
// neighbors, skipping sender AND original publisher). 2abOBFT shares
// the wiring shape; only the per-protocol builder closure differs.
type evtMeshArrival struct {
	from, to  ct.MeshNode
	publisher ct.MeshNode
	msgID     ct.MsgID
	kind      ct.MsgKind
	layer     int
	bytes     int64
	builder   func(to twoab.OperatorID) event
}

func (e *evtMeshArrival) describe() string {
	return ct.FormatMeshArrival(e.from, e.to, e.publisher, e.msgID, e.kind)
}

func (e *evtMeshArrival) handle(s *sim) []scheduledEvent {
	mesh := s.cfg.Mesh
	if !mesh.MarkSeen(e.to, e.msgID) {
		return nil
	}
	s.cacheArrivalForGossip(e.to, e.publisher, e.msgID, e.kind, e.layer, e.bytes, e.builder)
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
		if neighbor == e.from || neighbor == e.publisher {
			continue
		}
		delay := mesh.SampleHopDelay(s.rng, fromEP, mesh.EndpointFor(neighbor), e.kind)
		mesh.RecordMeshHop(s.cfg.Bandwidth, e.to, neighbor, e.kind, e.layer, e.bytes)
		out = append(out, scheduledEvent{
			when: s.now + mesh.ValidateDelay() + delay,
			ev: &evtMeshArrival{
				from:      e.to,
				to:        neighbor,
				publisher: e.publisher,
				msgID:     e.msgID,
				kind:      e.kind,
				layer:     e.layer,
				bytes:     e.bytes,
				builder:   e.builder,
			},
		})
	}
	return out
}

// ---- evtMeshHeartbeat / evtMeshIHave / evtMeshIWant --------------------
//
// Mirrors the OBFT adapter's gossip events; see protocol/v2/consensustest/
// obft/events.go evtMeshHeartbeat for the design rationale. Differences
// vs OBFT: the typed `builder` parameter uses twoab.OperatorID; otherwise
// the heartbeat / IHAVE / IWANT machinery is identical.

type evtMeshHeartbeat struct {
	node ct.MeshNode
}

func (e *evtMeshHeartbeat) describe() string {
	return fmt.Sprintf("MeshHeartbeat[node=%d]", e.node)
}

func (e *evtMeshHeartbeat) handle(s *sim) []scheduledEvent {
	mesh := s.cfg.Mesh
	g := mesh.Gossip()
	mesh.MCacheRotate(e.node)
	mids := mesh.MCacheGossipMids(e.node, g.HistoryGossip)
	if len(mids) == 0 {
		return nil
	}
	var out []scheduledEvent
	fromEP := mesh.EndpointFor(e.node)
	for _, to := range mesh.PickGossipRecipients(s.rng, e.node, g.Dlazy, g.GossipFactor) {
		toEP := mesh.EndpointFor(to)
		delay := s.cfg.Network.Delay(s.rng, fromEP, toEP, ct.KindGossipIHave)
		bytes := ct.GossipRPCSize(len(mids))
		mesh.RecordMeshHop(s.cfg.Bandwidth, e.node, to, ct.KindGossipIHave, -1, bytes)
		advertised := append([]ct.MsgID(nil), mids...)
		out = append(out, scheduledEvent{
			when: s.now + delay,
			ev:   &evtMeshIHave{from: e.node, to: to, mids: advertised},
		})
	}
	return out
}

type evtMeshIHave struct {
	from, to ct.MeshNode
	mids     []ct.MsgID
}

func (e *evtMeshIHave) describe() string {
	return fmt.Sprintf("MeshIHave[from=%d to=%d mids=%d]", e.from, e.to, len(e.mids))
}

func (e *evtMeshIHave) handle(s *sim) []scheduledEvent {
	mesh := s.cfg.Mesh
	var want []ct.MsgID
	for _, mid := range e.mids {
		if mesh.IsSeen(e.to, mid) {
			continue
		}
		want = append(want, mid)
	}
	if len(want) == 0 {
		return nil
	}
	fromEP := mesh.EndpointFor(e.to)
	toEP := mesh.EndpointFor(e.from)
	delay := s.cfg.Network.Delay(s.rng, fromEP, toEP, ct.KindGossipIWant)
	bytes := ct.GossipRPCSize(len(want))
	mesh.RecordMeshHop(s.cfg.Bandwidth, e.to, e.from, ct.KindGossipIWant, -1, bytes)
	return []scheduledEvent{{
		when: s.now + delay,
		ev:   &evtMeshIWant{from: e.to, to: e.from, mids: want},
	}}
}

type evtMeshIWant struct {
	from, to ct.MeshNode
	mids     []ct.MsgID
}

func (e *evtMeshIWant) describe() string {
	return fmt.Sprintf("MeshIWant[from=%d to=%d mids=%d]", e.from, e.to, len(e.mids))
}

func (e *evtMeshIWant) handle(s *sim) []scheduledEvent {
	mesh := s.cfg.Mesh
	for _, mid := range e.mids {
		entry, ok := mesh.MCacheLookup(e.to, mid)
		if !ok {
			continue
		}
		entry.Reinject(e.from)
	}
	return nil
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
			// Byz operator forging — synthesize a bundle directly, bypassing
			// the EKM σ-lock that BuildPhase1Bundle acquires (a byz leader
			// equivocating across multiple V's bypasses EKM exactly this way).
			// Sign L0Witness via direct signer access (only at L_0; L_k>0
			// leader witnesses are Phase D scope).
			var l0Witness twoab.Signature
			if e.layer == 0 {
				w, signErr := s.signers[leader].SignPartial(p.V)
				if signErr == nil {
					l0Witness = w
				}
				// If signing fails, leave L0Witness empty — ValidatePhase1Bundle
				// will reject the bundle at receivers, modeling a maximally-
				// malformed byz emission.
			}
			bundle = &twoab.Phase1Bundle{
				ClusterID:  s.cfgTwoab.ClusterID,
				OperatorID: leader,
				Height:     s.cfgTwoab.Height,
				Layer:      e.layer,
				Value:      append(twoab.Value{}, p.V...),
				L0Witness:  l0Witness,
			}
		} else {
			// Leader self-observes their own bundle so their own retention
			// state reflects V at this layer. Without self-observation the
			// leader's L_0 retention would be empty and they'd take the
			// NoValue path at their own Phase-2a fire-time. Post Op3 the
			// bundle also carries the leader's σ partial (L0Witness),
			// which is self-pooled into σ-pool[V_0] via the ObservePhase1Bundle
			// verify+pool path (the leader's own pool is updated as a
			// receiver of their own bundle).
			_ = s.instances[leader].ObservePhase1Bundle(bundle, s.observedOffset())
			leaderValid := s.cfg.Host.Validate(ct.OperatorID(leader), e.layer, p.V, ct.PhasePhase1Acceptance)
			_ = s.instances[leader].ApplyHostValidity(e.layer, p.V, leaderValid)
			// ApplyHostValidity's afterStateDelta cascade may have produced
			// an upgrade KindValue or a Phase-2b Commit at the leader
			// already (rare for a leader at Phase-1 time, but possible if
			// they had previously emitted KindNoValue from a different
			// layer's perspective via cluster-wide cascading state). Capture
			// any cascade emissions.
			out = append(out, captureCascadeEmissions(s, leader)...)
		}
		// 2abOBFT Phase 1 carries the leader's L0Witness σ partial at L_0
		// post Op3. The offline-aggregator path does NOT observe this
		// witness directly at leader-fetch time (the witness is verified
		// + pooled via the ObservePhase1Bundle receiver path on every
		// honest op). σ partials at L_k>0 are still credited via Phase-2a
		// LayerEntries (SigmaChained).
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
	// Per spec §Phase 1, 2abOBFT has no T_commit hard wall — bundles
	// are accepted at any in-slot offset. ObservePhase1Bundle returns
	// nil even for late bundles, modulo structural validation; the
	// only hard cutoff is the runner-level RelayCutoff.
	if err := inst.ObservePhase1Bundle(e.bundle, s.observedOffset()); err != nil {
		return nil
	}
	valid := s.cfg.Host.Validate(ct.OperatorID(e.to), e.layer, e.bundle.Value, ct.PhasePhase1Acceptance)
	_ = inst.ApplyHostValidity(e.layer, e.bundle.Value, valid)
	// Both ObservePhase1Bundle and ApplyHostValidity run the afterStateDelta
	// cascade internally (per spec §Emission ordering). Capture any
	// emissions the cascade produced (A1 upgrade ValueMsg or Phase-2b
	// Commit) and schedule per-recipient arrivals.
	return captureCascadeEmissions(s, e.to)
}

// ---- evtPhase2aFire ----------------------------------------------------

// evtPhase2aFire fires at cfg.TPhase2a. Every operator calls
// MaybeFirePhase2a, which returns one of {ValueMsg, NoValueMsg,
// Commit-NRDirect} per spec §Phase 2a. After firing, the
// afterStateDelta cascade inside MaybeFirePhase2a may have produced
// further emissions (Phase-2b Commit, A1 upgrade) — capture them all.
type evtPhase2aFire struct{}

func (e *evtPhase2aFire) describe() string { return "Phase2aFire" }

func (e *evtPhase2aFire) handle(s *sim) []scheduledEvent {
	var out []scheduledEvent
	for _, op := range s.operators {
		if !s.cfg.Byz.AllowPhase2aEmission(op) {
			continue
		}
		vm, nv, c, err := s.instances[op].MaybeFirePhase2a()
		if err != nil {
			continue
		}
		switch {
		case vm != nil:
			vm = s.cfg.Byz.OverrideValueMsg(s, op, vm)
			s.markValueMsgEmitted(op)
			out = append(out, scheduleValueMsg(s, op, vm)...)
		case nv != nil:
			nv = s.cfg.Byz.OverrideNoValueMsg(s, op, nv)
			s.markNoValueMsgEmitted(op)
			out = append(out, scheduleNoValueMsg(s, op, nv)...)
		case c != nil:
			c = s.cfg.Byz.OverrideCommit(s, op, c)
			s.markCommitEmitted(op)
			out = append(out, scheduleCommit(s, op, c)...)
		}
		// Universal BuildExtra* hooks: byz patterns may inject extras of
		// ANY Phase-2a kind regardless of which natural-kind fired —
		// enables A1/A2/A8 Rule-6a violation scenarios that mix kinds
		// (e.g., natural KindValue + extra KindNoValue downgrade). Each
		// hook receives the natural emission for the matching kind, or
		// nil for the other kinds. Honest defaults return nil; byz
		// patterns MUST nil-guard the natural-emission parameter.
		for _, extra := range s.cfg.Byz.BuildExtraValueMsgs(s, op, vm) {
			out = append(out, scheduleValueMsg(s, op, extra)...)
		}
		for _, extra := range s.cfg.Byz.BuildExtraNoValueMsgs(s, op, nv) {
			out = append(out, scheduleNoValueMsg(s, op, extra)...)
		}
		for _, extra := range s.cfg.Byz.BuildExtraCommits(s, op, c) {
			out = append(out, scheduleCommit(s, op, extra)...)
		}
		// Phase 2a fire may have cascaded into a Phase-2b Commit
		// emission (e.g., σ-eligibility trigger already satisfied at fire
		// time by earlier-cascading peer state). The Value/NoValue case
		// above hasn't yet marked the commit as emitted; capture it
		// here.
		out = append(out, captureCascadeEmissions(s, op)...)
		tryOpportunisticResolve(s, op)
	}
	return out
}

// ---- evtValueMsgArrival ------------------------------------------------

type evtValueMsgArrival struct {
	from, to twoab.OperatorID
	msg      *twoab.ValueMsg
}

func (e *evtValueMsgArrival) describe() string {
	return fmt.Sprintf("ValueMsgArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtValueMsgArrival) handle(s *sim) []scheduledEvent {
	inst := s.instances[e.to]
	_ = inst.ObserveValueMsg(e.msg)
	tryOpportunisticResolve(s, e.to)
	// ObserveValueMsg ran the afterStateDelta cascade — capture any
	// emissions that just fired (A1 upgrade or Phase-2b Commit).
	out := captureCascadeEmissions(s, e.to)
	// Spec-aligned late-arrival re-resolve: if past the slot cutoff and
	// not yet decided, retry Resolve.
	if s.now > s.resolveDeadline {
		if _, already := s.resolved[e.to]; !already {
			out = append(out, scheduledEvent{when: s.now, ev: &evtResolveRerun{op: e.to}})
		}
	}
	return out
}

// ---- evtNoValueMsgArrival ----------------------------------------------

type evtNoValueMsgArrival struct {
	from, to twoab.OperatorID
	msg      *twoab.NoValueMsg
}

func (e *evtNoValueMsgArrival) describe() string {
	return fmt.Sprintf("NoValueMsgArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtNoValueMsgArrival) handle(s *sim) []scheduledEvent {
	inst := s.instances[e.to]
	_ = inst.ObserveNoValueMsg(e.msg)
	tryOpportunisticResolve(s, e.to)
	out := captureCascadeEmissions(s, e.to)
	if s.now > s.resolveDeadline {
		if _, already := s.resolved[e.to]; !already {
			out = append(out, scheduledEvent{when: s.now, ev: &evtResolveRerun{op: e.to}})
		}
	}
	return out
}

// ---- evtCommitArrival --------------------------------------------------

type evtCommitArrival struct {
	from, to twoab.OperatorID
	commit   *twoab.Commit
}

func (e *evtCommitArrival) describe() string {
	return fmt.Sprintf("CommitArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtCommitArrival) handle(s *sim) []scheduledEvent {
	inst := s.instances[e.to]
	_ = inst.ObserveCommit(e.commit)
	tryOpportunisticResolve(s, e.to)
	out := captureCascadeEmissions(s, e.to)
	if s.now > s.resolveDeadline {
		if _, already := s.resolved[e.to]; !already {
			out = append(out, scheduledEvent{when: s.now, ev: &evtResolveRerun{op: e.to}})
		}
	}
	return out
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
	// op. Layer is unknown for cert-decided outputs; pass 0 so the
	// recorded time is exactly s.now (no walk cost).
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, e.to, 0, s.now, s.cfg.Epsilon3)
	delete(s.resolveErrs, e.to)
	return nil
}

// ---- cascade-emission tracking ----------------------------------------

// captureCascadeEmissions detects new emissions produced by an op's
// afterStateDelta cascade (run automatically inside every Observe*,
// MaybeFirePhase2a, and ApplyHostValidity call). For each detected
// emission, schedules per-recipient arrival events and records the
// emission's bytes in the bandwidth accumulator.
//
// Mechanism: track per-op flags (valueMsgEmitted / noValueMsgEmitted /
// commitEmitted) for the OWN-emission status. The Instance exposes
// OwnValueMsg() / OwnNoValueMsg() / OwnCommit() accessors; on every
// state-delta-triggering event handler, compare the current accessor
// status to the cached flag and schedule arrivals for any newly-flipped
// emission. The Instance enforces single-emission discipline internally;
// the adapter's tracking handles the propagation side.
func captureCascadeEmissions(s *sim, op twoab.OperatorID) []scheduledEvent {
	var out []scheduledEvent
	inst := s.instances[op]

	if vm, ok := inst.OwnValueMsg(); ok && !s.valueMsgEmitted[op] {
		// Pure Phase-2a-late A1 upgrade vs Phase-2a fire-time
		// emission: at fire-time, MaybeFirePhase2a applies the byz
		// override path explicitly. An upgrade fired from the cascade
		// here is the A1 path — apply the byz upgrade-override hook.
		vm = s.cfg.Byz.OverrideUpgradeValueMsg(s, op, vm)
		s.markValueMsgEmitted(op)
		out = append(out, scheduleValueMsg(s, op, vm)...)
	}
	if nv, ok := inst.OwnNoValueMsg(); ok && !s.noValueMsgEmitted[op] {
		s.markNoValueMsgEmitted(op)
		out = append(out, scheduleNoValueMsg(s, op, nv)...)
	}
	if c, ok := inst.OwnCommit(); ok && !s.commitEmitted[op] {
		// Phase-2b Commits emitted via the cascade also get the byz
		// override hook so adversarial scenarios can re-shape them
		// (e.g., inject forged plaintext σ partials for Rule 5).
		if c.Side != twoab.CommitSideNRDirect {
			c = s.cfg.Byz.OverrideCommit(s, op, c)
		}
		s.markCommitEmitted(op)
		out = append(out, scheduleCommit(s, op, c)...)
		for _, extra := range s.cfg.Byz.BuildExtraCommits(s, op, c) {
			out = append(out, scheduleCommit(s, op, extra)...)
		}
	}
	return out
}

// scheduleValueMsg broadcasts a ValueMsg from `from` to every other peer
// per byz delivery rules. Records aggregator observations for σ-chained
// LayerEntries. Returns scheduled events for direct-delivery mode;
// mesh-mode dispatches via s.emitMesh.
//
// Wire-kind: KindVerdict (shared with scheduleNoValueMsg) — see
// ct.KindVerdict comment in network.go for why both Phase-2a coordination
// kinds share one MsgKind bucket. Per-message byz overrides (delay,
// delivery filter) cannot distinguish ValueMsg from NoValueMsg via the
// MsgKind axis alone; patterns that need that distinction subclass the
// adapter-internal hook (Override{Value,NoValue}Msg + Build{Value,NoValue}Extra*).
func scheduleValueMsg(s *sim, from twoab.OperatorID, vm *twoab.ValueMsg) []scheduledEvent {
	if vm == nil {
		return nil
	}
	if s.cfg.Aggregator != nil {
		recordValueMsgToAggregator(s.cfg.Aggregator, vm)
	}
	bytes := valueMsgSize(vm)
	vmCap := vm
	fromCap := from
	build := func(to twoab.OperatorID) event {
		return &evtValueMsgArrival{from: fromCap, to: to, msg: cloneValueMsg(vmCap)}
	}
	if s.cfg.Mesh != nil {
		s.emitMesh(from, ct.KindVerdict, -1, bytes, 0, s.operators, build)
		return nil
	}
	var out []scheduledEvent
	for _, to := range s.operators {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, ct.KindVerdict) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.rng, from, to, ct.KindVerdict)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, ct.OperatorID(from), ct.OperatorID(to), ct.KindVerdict)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), ct.KindVerdict, -1, bytes)
		}
		out = append(out, scheduledEvent{
			when: s.now + delay,
			ev:   &evtValueMsgArrival{from: from, to: to, msg: cloneValueMsg(vm)},
		})
	}
	return out
}

// scheduleNoValueMsg broadcasts a NoValueMsg from `from` to every other
// peer. Wire-kind: KindVerdict (shared with scheduleValueMsg) — see
// scheduleValueMsg's comment for the rationale.
func scheduleNoValueMsg(s *sim, from twoab.OperatorID, nv *twoab.NoValueMsg) []scheduledEvent {
	if nv == nil {
		return nil
	}
	if s.cfg.Aggregator != nil {
		recordNoValueMsgToAggregator(s.cfg.Aggregator, nv)
	}
	bytes := noValueMsgSize(nv)
	nvCap := nv
	fromCap := from
	build := func(to twoab.OperatorID) event {
		return &evtNoValueMsgArrival{from: fromCap, to: to, msg: cloneNoValueMsg(nvCap)}
	}
	if s.cfg.Mesh != nil {
		s.emitMesh(from, ct.KindVerdict, -1, bytes, 0, s.operators, build)
		return nil
	}
	var out []scheduledEvent
	for _, to := range s.operators {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, ct.KindVerdict) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.rng, from, to, ct.KindVerdict)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, ct.OperatorID(from), ct.OperatorID(to), ct.KindVerdict)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), ct.KindVerdict, -1, bytes)
		}
		out = append(out, scheduledEvent{
			when: s.now + delay,
			ev:   &evtNoValueMsgArrival{from: from, to: to, msg: cloneNoValueMsg(nv)},
		})
	}
	return out
}

// scheduleCommit broadcasts a Commit from `from` to every other peer.
func scheduleCommit(s *sim, from twoab.OperatorID, c *twoab.Commit) []scheduledEvent {
	if c == nil {
		return nil
	}
	if s.cfg.Aggregator != nil {
		recordCommitToAggregator(s.cfg.Aggregator, c)
	}
	bytes := commitSize(c)
	cCap := c
	fromCap := from
	build := func(to twoab.OperatorID) event {
		return &evtCommitArrival{from: fromCap, to: to, commit: cloneCommit(cCap)}
	}
	extraDelay := s.cfg.Byz.OverrideOwnCommitDispatchDelay(s, from)
	if s.cfg.Mesh != nil {
		s.emitMesh(from, ct.KindCommit, -1, bytes, extraDelay, s.operators, build)
		return nil
	}
	var out []scheduledEvent
	for _, to := range s.operators {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, ct.KindCommit) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.rng, from, to, ct.KindCommit)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, ct.OperatorID(from), ct.OperatorID(to), ct.KindCommit)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), ct.KindCommit, -1, bytes)
		}
		out = append(out, scheduledEvent{
			when: s.now + delay + extraDelay,
			ev:   &evtCommitArrival{from: from, to: to, commit: cloneCommit(c)},
		})
	}
	return out
}

// recordValueMsgToAggregator records per-layer σ / encrypted-claim partials
// from a ValueMsg into the offline aggregator. ValueMsg carries no L_0
// σ partial (that's in the later Commit-Signed); only L_k>0 SigmaChained
// LayerEntries contribute (as encrypted claims). Credits the claimed
// sender's OperatorID, matching the byz-observer model from base OBFT.
func recordValueMsgToAggregator(agg *ct.OfflineAggregator, vm *twoab.ValueMsg) {
	from := ct.OperatorID(vm.OperatorID)
	for _, e := range vm.LayerEntries {
		switch e.Kind {
		case twoab.LayerEntrySigmaChained:
			agg.ObserveEncryptedClaim(from, e.Layer, e.Payload)
		case twoab.LayerEntryNRPlaintext:
			agg.ObserveNR(from, e.Layer)
		}
	}
}

// recordNoValueMsgToAggregator records per-layer σ / NR partials from a
// NoValueMsg into the offline aggregator. NoValueMsg has the same K-1
// LayerEntries shape as ValueMsg.
func recordNoValueMsgToAggregator(agg *ct.OfflineAggregator, nv *twoab.NoValueMsg) {
	from := ct.OperatorID(nv.OperatorID)
	for _, e := range nv.LayerEntries {
		switch e.Kind {
		case twoab.LayerEntrySigmaChained:
			agg.ObserveEncryptedClaim(from, e.Layer, e.Payload)
		case twoab.LayerEntryNRPlaintext:
			agg.ObserveNR(from, e.Layer)
		}
	}
}

// recordCommitToAggregator records per-layer σ / NR partials from a
// Commit into the offline aggregator. At L_0:
//   - Side=Signed: plaintext σ partial on L0Value → ObserveSigma at L_0.
//   - Side=NR or Side=NRDirect: NR tag partial at L_0 → ObserveNR at L_0.
//
// For Side=NRDirect only: LayerEntries are also recorded (mirroring the
// ValueMsg/NoValueMsg path) since the NRDirect emission bundles the K-1
// L_k>0 commitments with the L_0 emission.
func recordCommitToAggregator(agg *ct.OfflineAggregator, c *twoab.Commit) {
	from := ct.OperatorID(c.OperatorID)
	switch c.Side {
	case twoab.CommitSideSigned:
		agg.ObserveSigma(from, 0, c.L0Value)
	case twoab.CommitSideNR:
		agg.ObserveNR(from, 0)
	case twoab.CommitSideNRDirect:
		agg.ObserveNR(from, 0)
		for _, e := range c.LayerEntries {
			switch e.Kind {
			case twoab.LayerEntrySigmaChained:
				agg.ObserveEncryptedClaim(from, e.Layer, e.Payload)
			case twoab.LayerEntryNRPlaintext:
				agg.ObserveNR(from, e.Layer)
			}
		}
	}
}

// tryOpportunisticResolve mirrors the OBFT adapter's helper of the
// same name. See protocol/v2/consensustest/obft/events.go for the
// design rationale. 2abOBFT differs only in trigger events (Phase-2a
// emissions + Commits; not just Commits as in base) and the concrete
// Resolve impl walks the chained-NR ladder using both L_0 σ-pool entries
// (from Commit-Signed) and L_k>0 σ-chained entries (from ValueMsg /
// NoValueMsg / Commit-NRDirect LayerEntries).
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
