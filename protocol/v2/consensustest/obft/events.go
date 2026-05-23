package obft

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/desim"
	obftbase "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
)

// Events run on the shared desim.Engine; each event's Handle returns follow-on
// desim.Scheduled items. Ordering is (when, seq) — see desim.Engine.

// ---- evtMeshArrival ----------------------------------------------------

// evtMeshArrival is the mesh-transport delivery event. One per
// (publisher, msgID, hop): scheduled by emitMesh for first-hop neighbors
// of the publisher, and by its own handler for further hops via reflood.
// The handler dedup's by (to, msgID); on first arrival, delivers to the
// local protocol via builder (only when `to` is a cluster op, not a
// relay) and schedules forwards to every other mesh neighbor.
//
// The protocol arrival event (evtPhase1Arrival / evtCommitArrival /
// evtCertArrival) is constructed lazily via builder so we don't pay
// cloneXxx() at every forward hop — only the cluster op that consumes
// the message pays the clone cost. Layer / bytes are carried through
// for future per-hop bandwidth accounting; today's accounting records
// outbound at publish (in emitMesh) and at each forward (here).
//
// publisher is the MeshNode that originally emitted the message and is
// preserved across every hop. The forward loop skips both the immediate
// sender and the publisher, matching go-libp2p-pubsub's Publish (which
// excludes msg.GetFrom() — the original publisher — in addition to the
// relay sender).
type evtMeshArrival struct {
	from, to  ct.MeshNode
	publisher ct.MeshNode
	msgID     ct.MsgID
	kind      ct.MsgKind
	layer     int
	bytes     int64
	builder   func(to obftbase.OperatorID) desim.Event
}

func (e *evtMeshArrival) Describe() string {
	return ct.FormatMeshArrival(e.from, e.to, e.publisher, e.msgID, e.kind)
}

func (e *evtMeshArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	mesh := s.cfg.Mesh
	// Dedup. Mesh.MarkSeen returns false on duplicate; libp2p drops the
	// message at this point without forwarding (its dedup-cache hit
	// short-circuits the propagation step).
	if !mesh.MarkSeen(e.to, e.msgID) {
		return nil
	}
	// Stash a reinject closure on this receiver's mcache so it can
	// answer future IWANT requests for this body. No-op when gossip is
	// disabled (s.cacheArrivalForGossip checks).
	s.cacheArrivalForGossip(e.to, e.publisher, e.msgID, e.kind, e.layer, e.bytes, e.builder)
	var out []desim.Scheduled
	// Deliver to local protocol when `to` is a cluster op. Relay peers
	// only forward — they have no protocol state. Apply per-hop
	// ValidateDelay between the wire-arrival and the protocol-state
	// update; default 0 (mesh validation is modeled as fast).
	if mesh.IsProtocol(e.to) {
		recipientOp := obftbase.OperatorID(mesh.OperatorForNode(e.to))
		out = append(out, desim.Scheduled{
			When: s.Now() + mesh.ValidateDelay(),
			Ev:   e.builder(recipientOp),
		})
	}
	// Forward to every other mesh neighbor, skipping (a) the immediate
	// sender — basic loop prevention; and (b) the original publisher —
	// matches go-libp2p-pubsub's Publish, which excludes msg.GetFrom()
	// even when the relay sender differs. The dedup cache catches any
	// remaining cycles.
	fromEP := mesh.EndpointFor(e.to)
	for _, neighbor := range mesh.Neighbors(e.to) {
		if neighbor == e.from || neighbor == e.publisher {
			continue
		}
		delay := mesh.SampleHopDelay(s.Rng(), fromEP, mesh.EndpointFor(neighbor), e.kind)
		// Bandwidth accounting: each hop's wire bytes count toward
		// TotalBytes regardless of who's forwarding (libp2p re-flood
		// puts the bytes on the wire whether the hop is cluster-side
		// or relay-side). The four-way dispatch in RecordMeshHop
		// keeps PerOperator* charged only to cluster ops.
		mesh.RecordMeshHop(s.cfg.Bandwidth, e.to, neighbor, e.kind, e.layer, e.bytes)
		out = append(out, desim.Scheduled{
			When: s.Now() + mesh.ValidateDelay() + delay,
			Ev: &evtMeshArrival{
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
// Gossipsub lazy-push backstop layered on top of the eager-mesh
// transport:
//
//   - Every cfg.Mesh.Gossip.HeartbeatInterval, each mesh node ticks.
//     It rotates its mcache (evicting the oldest slot) and emits
//     evtMeshIHave to a stable-random subset of its non-mesh peers,
//     advertising the mids it cached in the last HistoryGossip slots.
//   - An evtMeshIHave at a node where some advertised mids are unseen
//     produces one evtMeshIWant back, listing the unseen mids.
//   - An evtMeshIWant at a node looks each requested mid up in its
//     mcache; for hits, the entry's reinject closure schedules a
//     fresh evtMeshArrival from this node to the requester. The
//     requester's MarkSeen gate de-dupes against a copy that arrived
//     via mesh in the meantime.
//
// IHAVE / IWANT transit single direct hops, not the mesh chain — real
// gossipsub rides direct TCP connections for control RPCs. Bandwidth
// is accounted through mesh.RecordMeshHop with KindGossipIHave /
// KindGossipIWant so the per-(from, to) histograms include gossip
// traffic alongside eager-mesh hops.

type evtMeshHeartbeat struct {
	node ct.MeshNode
}

func (e *evtMeshHeartbeat) Describe() string {
	return fmt.Sprintf("MeshHeartbeat[node=%d]", e.node)
}

func (e *evtMeshHeartbeat) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	mesh := s.cfg.Mesh
	g := mesh.Gossip()
	// Rotate first so the evicted slot is the OLDEST and the IHAVE
	// window read picks up only mids still within the HistoryGossip
	// retention budget.
	mesh.MCacheRotate(e.node)
	mids := mesh.MCacheGossipMids(e.node, g.HistoryGossip)
	if len(mids) == 0 {
		return nil
	}
	var out []desim.Scheduled
	fromEP := mesh.EndpointFor(e.node)
	for _, to := range mesh.PickGossipRecipients(s.Rng(), e.node, g.Dlazy, g.GossipFactor) {
		toEP := mesh.EndpointFor(to)
		delay := s.cfg.Network.Delay(s.Rng(), fromEP, toEP, ct.KindGossipIHave)
		bytes := ct.GossipRPCSize(len(mids))
		mesh.RecordMeshHop(s.cfg.Bandwidth, e.node, to, ct.KindGossipIHave, -1, bytes)
		// Defensive copy: PickGossipRecipients reuses the pool slice
		// across iterations but MCacheGossipMids returns a fresh slice
		// each call; we still copy so the event holds a stable view
		// of what was advertised at heartbeat time.
		advertised := append([]ct.MsgID(nil), mids...)
		out = append(out, desim.Scheduled{
			When: s.Now() + delay,
			Ev:   &evtMeshIHave{from: e.node, to: to, mids: advertised},
		})
	}
	return out
}

type evtMeshIHave struct {
	from, to ct.MeshNode
	mids     []ct.MsgID
}

func (e *evtMeshIHave) Describe() string {
	return fmt.Sprintf("MeshIHave[from=%d to=%d mids=%d]", e.from, e.to, len(e.mids))
}

func (e *evtMeshIHave) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
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
	delay := s.cfg.Network.Delay(s.Rng(), fromEP, toEP, ct.KindGossipIWant)
	bytes := ct.GossipRPCSize(len(want))
	mesh.RecordMeshHop(s.cfg.Bandwidth, e.to, e.from, ct.KindGossipIWant, -1, bytes)
	return []desim.Scheduled{{
		When: s.Now() + delay,
		Ev:   &evtMeshIWant{from: e.to, to: e.from, mids: want},
	}}
}

type evtMeshIWant struct {
	from, to ct.MeshNode
	mids     []ct.MsgID
}

func (e *evtMeshIWant) Describe() string {
	return fmt.Sprintf("MeshIWant[from=%d to=%d mids=%d]", e.from, e.to, len(e.mids))
}

func (e *evtMeshIWant) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	mesh := s.cfg.Mesh
	for _, mid := range e.mids {
		entry, ok := mesh.MCacheLookup(e.to, mid)
		if !ok {
			// Real gossipsub silently drops requests for missing mids
			// (the requester will hear the mid from another peer next
			// heartbeat). Mirror that — no penalty / no error.
			continue
		}
		// The reinject closure schedules a fresh evtMeshArrival
		// directly on s.queue (via the captured *sim). We return no
		// scheduled events ourselves; the closure's effect lands in
		// the queue the same way an emit-time s.schedule would.
		entry.Reinject(e.from)
	}
	return nil
}

// ---- evtLeaderFetch ----------------------------------------------------

type evtLeaderFetch struct {
	layer int
}

func (e *evtLeaderFetch) Describe() string { return fmt.Sprintf("LeaderFetch[layer=%d]", e.layer) }

func (e *evtLeaderFetch) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	leader := s.cfgObft.Layers[e.layer].Leader
	plans := s.cfg.Byz.LeaderBroadcastPlan(s, leader, e.layer, s.honestLeaderValue(e.layer))

	var out []desim.Scheduled
	for _, p := range plans {
		bundle, err := s.instances[leader].BuildPhase1Bundle(e.layer, p.V)
		if err != nil {
			bundle = s.forgeByzBundle(leader, e.layer, p.V)
		} else {
			// Leader's own host verdict for V at this layer — same query the
			// receivers issue at evtPhase1Arrival. Without this, a host-invalid
			// scenario (e.g. HostInvalidUntilLayer) would have receivers
			// correctly NV/NR while the leader's instance recorded host-valid
			// and σ-emitted, breaking the model's "all NR" symmetry.
			leaderValid := s.cfg.Host.Validate(ct.OperatorID(leader), e.layer, p.V, ct.PhasePhase1Acceptance)
			_ = s.instances[leader].ApplyHostValidity(e.layer, p.V, leaderValid)
		}
		// OfflineAggregator: leader's σ_L^V partial hits the wire.
		if s.cfg.Aggregator != nil {
			s.cfg.Aggregator.ObserveSigma(ct.OperatorID(leader), e.layer, bundle.Value)
		}
		bundleBytes := phase1BundleSize(bundle)
		// Per-leader delay added on top of the per-pair network delay; used
		// by byzLateLeaderBroadcast to push the entire emission past T_commit.
		ownDelay := s.cfg.Byz.OverrideOwnPhase1Delay(s, leader)
		recipients := p.Recipients
		if recipients == nil {
			recipients = s.operators
		}
		bundleCap := bundle
		layerCap := e.layer
		build := func(to obftbase.OperatorID) desim.Event {
			return &evtPhase1Arrival{from: leader, to: to, layer: layerCap, bundle: clonePhase1Bundle(bundleCap)}
		}
		if s.cfg.Mesh != nil {
			// Mesh path: emitMesh schedules first-hop arrivals via
			// s.schedule (out of band from this handle's return), then
			// dedup'd reflood reaches every connected protocol peer in
			// `recipients`. Note that mesh ignores the per-recipient
			// Recipients filter past the first hop — re-flooding
			// neighbors will deliver to suppressed receivers anyway;
			// the documented mesh-vs-direct trade-off.
			s.emitMesh(leader, ct.KindLeaderBroadcast, e.layer, bundleBytes, ownDelay, recipients, build)
			continue
		}
		for _, to := range recipients {
			if to == leader {
				continue
			}
			delay := s.cfg.Byz.OverrideDelay(s.Rng(), leader, to, ct.KindLeaderBroadcast)
			if delay < 0 {
				delay = s.cfg.Network.Delay(s.Rng(), ct.OperatorID(leader), ct.OperatorID(to), ct.KindLeaderBroadcast)
			}
			if s.cfg.Bandwidth != nil && bundleBytes > 0 {
				s.cfg.Bandwidth.Emission(ct.OperatorID(leader), ct.OperatorID(to),
					ct.KindLeaderBroadcast, e.layer, bundleBytes)
			}
			out = append(out, desim.Scheduled{
				When: s.Now() + delay + ownDelay,
				Ev:   &evtPhase1Arrival{from: leader, to: to, layer: e.layer, bundle: clonePhase1Bundle(bundle)},
			})
		}
	}
	// Spec §Phase 2 emission-timing: the L_0 leader's BuildPhase1Bundle
	// sets sigmaLocked[0], closing L0ReadyCh on the leader-σ_V branch.
	// Fire the early commit emit for the leader so their KindCommit hits
	// the wire alongside (or before) their Phase-1 bundle arrivals at
	// peers — letting V-drop receivers exercise the peer-reflood-V path.
	if e.layer == 0 {
		out = append(out, maybeEarlyCommit(s, leader)...)
	}
	return out
}

func (s *sim) forgeByzBundle(leader obftbase.OperatorID, layer int, v obftbase.Value) *obftbase.Phase1Bundle {
	var signer obftbase.Signer
	if s.cfg.BLSKeys != nil {
		share := s.cfg.BLSKeys.Shares[ct.OperatorID(leader)]
		signer = blsbackend.New(share)
	} else {
		q := s.cfgObft.QV()
		signer = obftbase.NewStubSigner(q, []byte{byte(leader)})
	}
	sig, err := signer.SignPartial(v)
	if err != nil {
		panic(fmt.Sprintf("obft adapter: forge bundle for leader %d: %v", leader, err))
	}
	return &obftbase.Phase1Bundle{
		ClusterID:   s.cfgObft.ClusterID,
		OperatorID:  leader,
		Height:      s.cfgObft.Height,
		Layer:       layer,
		Value:       append(obftbase.Value{}, v...),
		LeaderSigma: sig,
	}
}

// ---- evtPhase1Arrival --------------------------------------------------

type evtPhase1Arrival struct {
	from, to obftbase.OperatorID
	layer    int
	bundle   *obftbase.Phase1Bundle
}

func (e *evtPhase1Arrival) Describe() string {
	return fmt.Sprintf("Phase1Arrival[from=%d to=%d layer=%d v=%s]",
		e.from, e.to, e.layer, valuePrefix(e.bundle.Value))
}

func (e *evtPhase1Arrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	inst := s.instances[e.to]
	if err := inst.ObservePhase1Bundle(e.bundle, s.observedOffset()); err != nil {
		return nil
	}
	valid := s.cfg.Host.Validate(ct.OperatorID(e.to), e.layer, e.bundle.Value, ct.PhasePhase1Acceptance)
	_ = inst.ApplyHostValidity(e.layer, e.bundle.Value, valid)
	// Spec §Phase 2 emission-timing: ApplyHostValidity for L_0 may close
	// L0ReadyCh on the Phase-1 retention σ path; if so, fire the early
	// commit emit now rather than waiting for the T_commit fallback.
	return maybeEarlyCommit(s, e.to)
}

// ---- evtPhaseTwoStart --------------------------------------------------

type evtPhaseTwoStart struct{}

func (e *evtPhaseTwoStart) Describe() string { return "PhaseTwoStart" }

// evtPhaseTwoStart is the T_commit fallback that fires emitOwnCommit for
// every op that hasn't already emitted via the L0Ready-driven early-commit
// path (sim.commitEmitted guard). Operators whose L_0 decision became
// determinable before T_commit (Phase-1 retention + host valid, leader's
// own BuildPhase1Bundle, observed equivocation, or peer-reflood-V at L_0)
// fire their commit at the moment L0Ready closes via maybeEarlyCommit
// scheduled from evtPhase1Broadcast / evtPhase1Arrival / evtCommitArrival.
func (e *evtPhaseTwoStart) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	for _, op := range s.operators {
		emitOwnCommit(s, op)
	}
	return nil
}

// ---- evtCommitEmit -----------------------------------------------------

// evtCommitEmit fires for a single op at the moment their L_0 decision
// becomes determinable — the spec §Phase 2 emission-timing early-emit
// trigger. Scheduled by maybeEarlyCommit from evtPhase1Broadcast (L_0
// leader's own bundle), evtPhase1Arrival (Phase-1 retention path), and
// evtCommitArrival (peer-reflood-V path).
type evtCommitEmit struct {
	op obftbase.OperatorID
}

func (e *evtCommitEmit) Describe() string {
	return fmt.Sprintf("CommitEmit[op=%d]", e.op)
}

func (e *evtCommitEmit) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	emitOwnCommit(s, e.op)
	return nil
}

// emitOwnCommit dispatches op's KindCommit if it hasn't been emitted yet
// AND the byz pattern allows it. Idempotent via the commitEmitted guard:
// later calls (e.g. evtPhaseTwoStart T_commit fallback for an op that
// already early-emitted) are no-ops.
func emitOwnCommit(s *sim, op obftbase.OperatorID) {
	if s.commitEmitted[op] {
		return
	}
	if !s.cfg.Byz.AllowCommitBroadcast(op) {
		// Mark emitted to suppress future T_commit fallback attempts;
		// the byz suppression is permanent for this slot.
		s.commitEmitted[op] = true
		return
	}
	c, err := s.instances[op].BuildOwnCommit()
	if err != nil || c == nil {
		// Most-common err here is ErrInstanceEnded for a stale-slot
		// fallback — mark emitted so we don't retry.
		s.commitEmitted[op] = true
		return
	}
	s.commitEmitted[op] = true
	c = s.cfg.Byz.OverrideCommit(s, op, c)
	// OfflineAggregator: op's Commit hits the wire — record per-layer
	// σ side (plaintext at L_0, encrypted-claim at L_k>0) and per-NR.
	if s.cfg.Aggregator != nil {
		recordCommitToAggregator(s.cfg.Aggregator, c)
	}
	// Byz patterns may delay their own KindCommit dispatch to land past
	// RoundEndOffset, exercising the spec §Phase 3 late-arrival
	// re-resolve recovery path.
	extraDelay := s.cfg.Byz.OverrideOwnCommitDispatchDelay(s, op)
	s.emitToAll(op, ct.KindCommit, -1, commitSize(c), extraDelay, func(to obftbase.OperatorID) desim.Event {
		return &evtCommitArrival{from: op, to: to, commit: cloneCommit(c)}
	})
	// Byz patterns may add ADDITIONAL commits (e.g. cross-onion
	// equivocation emits two structurally-distinct Commits).
	for _, extra := range s.cfg.Byz.BuildExtraCommits(s, op, c) {
		extra := extra
		if s.cfg.Aggregator != nil {
			recordCommitToAggregator(s.cfg.Aggregator, extra)
		}
		s.emitToAll(op, ct.KindCommit, -1, commitSize(extra), extraDelay, func(to obftbase.OperatorID) desim.Event {
			return &evtCommitArrival{from: op, to: to, commit: cloneCommit(extra)}
		})
	}
	// Observer-mode self-resolve probe: BuildOwnCommit self-observes
	// the op's own σ/NR partials into its local pools, so an op that
	// is itself the L_0 leader at f=0 (degenerate cluster size n=1,
	// qV=1) can satisfy σ-quorum from its own self-observation alone.
	// At realistic f≥1 the probe returns ErrNoQuorum (1 partial < qV)
	// and is a no-op. Cheap insurance against future degenerate-fixture
	// regressions.
	tryOpportunisticResolve(s, op)
}

// maybeEarlyCommit checks whether op's L_0 decision is now determinable
// (L0ReadyCh closed) and schedules an evtCommitEmit at the current sim
// time if so. Spec §Phase 2 emission-timing.
//
// Called from event handlers that can close L0ReadyCh: evtLeaderFetch
// (after BuildPhase1Bundle for the L_0 leader's own σ-lock), evtPhase1Arrival
// (after ApplyHostValidity for L_0 via Phase-1 retention path), and
// evtCommitArrival (after the host-validation drain for the peer-reflood-V
// path).
//
// Idempotent: returns nil if op has already emitted or if L0Ready hasn't
// closed yet.
func maybeEarlyCommit(s *sim, op obftbase.OperatorID) []desim.Scheduled {
	if s.commitEmitted[op] {
		return nil
	}
	select {
	case <-s.instances[op].L0ReadyCh():
		return []desim.Scheduled{{
			When: s.Now(),
			Ev:   &evtCommitEmit{op: op},
		}}
	default:
		return nil
	}
}

// recordCommitToAggregator extracts every σ / NR / encrypted-onion partial
// from c and records it in the aggregator. Models the "byz observes every
// Commit dispatched on the wire" assumption — credits c.OperatorID (the
// claimed sender on the wire), NOT the actual emitter. Honest commits
// have c.OperatorID == emitter so this is identity; byz commits with
// forged c.OperatorID get credited to the forged identity, which is the
// adversary-observable view by design (validates the safety machinery
// against forged-identity attacks via byzAggregatorBypass).
//
// At L_0 the EncryptedLayer.Ciphertext holds the plaintext σ partial bytes
// directly (no IBE wrapping); deeper layers carry chained-IBE ciphertext.
// Layer index drives the classification — not Ciphertext-emptiness.
func recordCommitToAggregator(agg *ct.OfflineAggregator, c *obftbase.Commit) {
	from := ct.OperatorID(c.OperatorID)
	for layer, el := range c.Layers {
		if len(el.Value) == 0 {
			continue
		}
		if layer == 0 {
			agg.ObserveSigma(from, layer, el.Value)
		} else {
			agg.ObserveEncryptedClaim(from, layer, el.Value)
		}
	}
	for _, nr := range c.NRPartials {
		agg.ObserveNR(from, nr.Layer)
	}
	for _, w := range c.Witnesses {
		agg.ObserveSigmaByValueRoot(ct.OperatorID(w.Leader), w.Layer, w.ValueRoot)
	}
}

// ---- evtCommitArrival --------------------------------------------------

type evtCommitArrival struct {
	from, to obftbase.OperatorID
	commit   *obftbase.Commit
}

func (e *evtCommitArrival) Describe() string {
	return fmt.Sprintf("CommitArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtCommitArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	inst := s.instances[e.to]
	_ = inst.ObserveCommit(e.commit)

	// Spec §Phase 2 / Peer-reflood V via early commit: drain any
	// host-validation requests the Instance enqueued (V's first-observed
	// via this peer commit's σ-onion entry at L_0 without an existing
	// host verdict). Mirrors the runner's drain-goroutine pattern in
	// protocol/v2/ssv/runner/obft/runner.go drainHostValidationRequests.
drainLoop:
	for {
		select {
		case req := <-inst.WantsHostValidationCh():
			valid := s.cfg.Host.Validate(ct.OperatorID(e.to), req.Layer, req.Value, ct.PhasePhase1Acceptance)
			_ = inst.ApplyHostValidity(req.Layer, req.Value, valid)
		default:
			break drainLoop
		}
	}
	// Spec §Phase 2 emission-timing: ObserveCommit + drained
	// ApplyHostValidity may close L0ReadyCh on the peer-reflood-V σ path
	// (V-drop receiver harvested V from this peer's σ-onion). If so,
	// fire the early commit emit now.
	earlyEmit := maybeEarlyCommit(s, e.to)

	// Observer-mode quorum detection: probe Resolve at the receiver
	// immediately on every commit arrival. First-success records
	// vQuorumAt[e.to] = s.Now() — the earliest moment this op holds a
	// submittable σ-cert. Resolve is stateless / idempotent (spec §Phase
	// 3) so probing on every arrival is safe and cheap. The Outcome
	// layer reads vQuorumAt as the BTT-sensitive DecisionTime, matching
	// QBFT's post-consensus partial-sig quorum semantic.
	tryOpportunisticResolve(s, e.to)

	// Spec §Phase 3 / "Re-running on late KindCommit arrivals": if this
	// commit landed past RoundEndOffset, the receiver re-runs Resolve to
	// incorporate the new partial. Skip if the receiver already decided.
	if s.Now() <= s.cfgObft.RoundEndOffset() {
		return earlyEmit
	}
	if _, already := s.resolved[e.to]; already {
		return earlyEmit
	}
	// Schedule the re-resolve immediately at the current sim time so the new
	// state is incorporated before any further events fire at this timestamp.
	return append(earlyEmit, desim.Scheduled{When: s.Now(), Ev: &evtResolveRerun{op: e.to}})
}

// tryOpportunisticResolve probes Resolve at `op` and records the first
// successful resolve time in s.vQuorumAt via the framework-level
// RecordFirstOpportunisticQuorum helper. Mirrors what an observer-mode
// production runner does — call Resolve on every state delta, submit as
// soon as σ-quorum reaches. Returns nothing — the metric is recorded as
// a side effect; the actual decided-value plumbing (s.resolved /
// s.resolvedAt) is left to the schedule-anchored evtResolve path or to
// evtCertArrival's cert-rescue path.
//
// Fast-paths the dedup before calling Resolve so a per-arrival call
// doesn't pay the walk-cost when σ-quorum was already captured earlier
// in the slot.
func tryOpportunisticResolve(s *sim, op obftbase.OperatorID) {
	if _, already := s.vQuorumAt[op]; already {
		return
	}
	res, err := s.instances[op].Resolve()
	if err != nil {
		return // ErrNoQuorum or transient — quorum not yet reached
	}
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, op, res.Layer, s.Now(), s.cfg.Epsilon3)
}

// ---- evtResolve --------------------------------------------------------

type evtResolve struct{}

func (e *evtResolve) Describe() string { return "Resolve" }

func (e *evtResolve) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	var out []desim.Scheduled
	for _, op := range s.operators {
		out = append(out, resolveOpAndBroadcastCert(s, op)...)
	}
	return out
}

// ---- evtResolveRerun ---------------------------------------------------

// evtResolveRerun re-runs Phase-3 resolve for a single operator that
// received a late KindCommit (after RoundEndOffset). Mirrors evtResolve's
// per-op flow: Resolve → record result → broadcast cert if successful.
//
// Spec §Phase 3: production obft.Instance.Resolve() is stateless / idempotent,
// so re-invocation with additional observed partials is safe (Pigeonholes 1+2
// guarantee at most one V reaches qV cluster-wide regardless of timing).
type evtResolveRerun struct {
	op obftbase.OperatorID
}

func (e *evtResolveRerun) Describe() string {
	return fmt.Sprintf("ResolveRerun[op=%d]", e.op)
}

func (e *evtResolveRerun) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	// Op may have decided between rerun scheduling and rerun firing — e.g.,
	// a cert from a peer's earlier successful reconstruction arrived in
	// between. Skip the rerun in that case so resolvedAt[op] reflects the
	// earlier (cert-gossip) decision time, not the later rerun time.
	if _, decided := s.resolved[e.op]; decided {
		return nil
	}
	return resolveOpAndBroadcastCert(s, e.op)
}

// resolveOpAndBroadcastCert runs Resolve for `op`, records the outcome in
// the sim's resolved/resolveErrs maps, and (on success) schedules cert
// broadcast to peers. Shared by evtResolve (initial pass at RoundEndOffset)
// and evtResolveRerun (late-commit re-attempt).
//
// On success, clears any prior resolveErrs entry so the late-recovered op
// reports decided=true with no Err in the outcome.
func resolveOpAndBroadcastCert(s *sim, op obftbase.OperatorID) []desim.Scheduled {
	res, err := s.instances[op].Resolve()
	if err != nil {
		// Record error only if op hasn't decided yet (e.g. via prior cert
		// gossip). Don't overwrite an existing success with a transient error
		// from a re-resolve.
		if _, decided := s.resolved[op]; !decided {
			s.resolveErrs[op] = err
		}
		return nil
	}
	s.resolved[op] = res
	// Phase-3 walk cost grows linearly with the number of layers visited
	// (OBFT.md §Phase 3: "ε_3 scales with the number of layers actually
	// walked ... ~ε_3 × K at K=4 with K−1 silent layers"). evtResolve fires
	// at RoundEndOffset = T_commit + Δ_2 + ε_3, which is the post-Phase-3
	// time assuming single-layer reconstruction at L_0. For fall-through
	// to L_k, add k × ε_3 to model the per-layer chained-decryption +
	// aggregation walks. At L_0 the extra cost is zero (single-layer
	// walk already in the base ε_3 budget).
	decisionTime := s.Now() + time.Duration(res.Layer)*s.cfg.Epsilon3
	s.resolvedAt[op] = decisionTime
	// Schedule-anchored fallback for the observer-mode metric: if no
	// prior commit/cert arrival already set vQuorumAt (i.e., L_0
	// σ-quorum never reached via Phase-2 propagation alone), capture
	// the schedule-anchored decision time here. Ensures every decided
	// op has a vQuorumAt entry the outcome layer can read uniformly.
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, op, res.Layer, s.Now(), s.cfg.Epsilon3)
	// Late-resolve success supersedes any prior error.
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
	build := func(to obftbase.OperatorID) desim.Event {
		return &evtCertArrival{from: opCap, to: to, cert: cloneCertificate(certCap)}
	}
	// Cert is broadcast immediately after this op's local decision, not
	// at evtResolve's fire time (which can be earlier when the per-layer
	// walk cost shifted decisionTime past s.Now()). Express the shift as
	// an extraDelay = (decisionTime − s.Now()) so the first-hop schedule
	// reads `s.Now() + edgeDelay + extraDelay = decisionTime + edgeDelay`,
	// matching the pre-refactor direct-mode behavior. Non-negative by
	// construction (decisionTime = s.Now() + Layer·Epsilon3, Layer ≥ 0).
	extraDelay := decisionTime - s.Now()
	if s.cfg.Mesh != nil {
		s.emitMesh(op, ct.KindCertificate, -1, certBytes, extraDelay, s.operators, build)
		return nil
	}
	var out []desim.Scheduled
	for _, to := range s.operators {
		if to == op {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(op, to, ct.KindCertificate) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.Rng(), op, to, ct.KindCertificate)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.Rng(), ct.OperatorID(op), ct.OperatorID(to), ct.KindCertificate)
		}
		if s.cfg.Bandwidth != nil && certBytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(op), ct.OperatorID(to),
				ct.KindCertificate, -1, certBytes)
		}
		out = append(out, desim.Scheduled{
			When: decisionTime + delay,
			Ev:   &evtCertArrival{from: op, to: to, cert: cloneCertificate(cert)},
		})
	}
	return out
}

// ---- evtCertArrival ----------------------------------------------------

type evtCertArrival struct {
	from, to obftbase.OperatorID
	cert     *obftbase.Certificate
}

func (e *evtCertArrival) Describe() string {
	return fmt.Sprintf("CertArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtCertArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	inst := s.instances[e.to]
	if err := inst.ObserveCertificate(e.cert); err != nil {
		return nil
	}
	if _, already := s.resolved[e.to]; already {
		return nil
	}
	s.resolved[e.to] = &obftbase.Output{
		Layer:     -1,
		Value:     append(obftbase.Value{}, e.cert.Value...),
		Signature: append(obftbase.Signature{}, e.cert.Signature...),
	}
	s.resolvedAt[e.to] = s.Now()
	// Cert receipt is itself the earliest "submittable" moment for this
	// op (the cert IS a complete cluster signature; no further local
	// resolve work is required). Capture it in vQuorumAt — mirrors how a
	// production observer-mode runner would short-circuit Resolve and
	// submit directly upon cert receipt (tryCertFastPath). Layer is
	// unknown for cert-decided outputs; pass 0 so the recorded time is
	// exactly s.Now() (no walk cost).
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, e.to, 0, s.Now(), s.cfg.Epsilon3)
	// Cert-gossip rescue supersedes a prior local-resolve failure; clear the
	// stale error so outcome() doesn't report decided=true with a non-empty
	// Err (otherwise confuses callers reading the per-op outcome).
	delete(s.resolveErrs, e.to)
	return nil
}
