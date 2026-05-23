package twoab

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/desim"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Events run on the shared desim.Engine; each event's Handle returns follow-on
// desim.Scheduled items. Ordering is (when, seq) — see desim.Engine.

// ---- evtLeaderFetch ----------------------------------------------------

type evtLeaderFetch struct {
	layer int
}

func (e *evtLeaderFetch) Describe() string { return fmt.Sprintf("LeaderFetch[layer=%d]", e.layer) }

func (e *evtLeaderFetch) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	leader := s.cfgTwoab.Layers[e.layer].Leader
	plans := s.cfg.Byz.LeaderBroadcastPlan(s, leader, e.layer, s.honestLeaderValue(e.layer))

	var out []desim.Scheduled
	for _, p := range plans {
		bundle, err := s.instances[leader].BuildPhase1Bundle(e.layer, p.V)
		if err != nil {
			// Byz operator forging — synthesize a bundle directly, bypassing
			// the EKM σ-lock that BuildPhase1Bundle acquires (a byz leader
			// equivocating across multiple V's bypasses EKM exactly this way).
			// Sign LeaderSigma via direct signer access at this layer (every
			// layer's leader carries a witness, not just L_0).
			var lWitness twoab.Signature
			w, signErr := s.signers[leader].SignPartial(p.V)
			if signErr == nil {
				lWitness = w
			}
			// If signing fails, leave LeaderSigma empty — ValidatePhase1Bundle
			// will reject the bundle at receivers, modeling a maximally-
			// malformed byz emission.
			bundle = &twoab.Phase1Bundle{
				ClusterID:   s.cfgTwoab.ClusterID,
				OperatorID:  leader,
				Height:      s.cfgTwoab.Height,
				Layer:       e.layer,
				Value:       append(twoab.Value{}, p.V...),
				LeaderSigma: lWitness,
			}
		} else {
			// Leader self-observes their own bundle so their own retention
			// state reflects V at this layer. Without self-observation the
			// leader's L_0 retention would be empty and they'd take the
			// NoValue path at their own Phase-2a fire-time. The bundle
			// also carries the leader's σ partial (LeaderSigma),
			// which is self-pooled into σ-pool[V_k] via the ObservePhase1Bundle
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
			// The leader's self-observe (retention + host valid)
			// closes its L0Ready at L_0 — async-fire its KindValue now,
			// well before the TPhase2a backstop (this is the earliest
			// σ-emission in the cluster, seeding σ-pool[V_0] for peers).
			out = append(out, maybeEarlyFire(s, leader)...)
		}
		// 2abOBFT Phase 1 carries the leader's LeaderSigma σ partial at every
		// layer. The offline-aggregator path does NOT observe this
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
		build := func(to twoab.OperatorID) desim.Event {
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
	return out
}

// ---- evtPhase1Arrival --------------------------------------------------

type evtPhase1Arrival struct {
	from, to twoab.OperatorID
	layer    int
	bundle   *twoab.Phase1Bundle
}

func (e *evtPhase1Arrival) Describe() string {
	return fmt.Sprintf("Phase1Arrival[from=%d to=%d layer=%d v=%s]",
		e.from, e.to, e.layer, valuePrefix(e.bundle.Value))
}

func (e *evtPhase1Arrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
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
	// emissions the cascade produced (upgrade ValueMsg or Phase-2b
	// Commit) and schedule per-recipient arrivals.
	out := captureCascadeEmissions(s, e.to)
	// Bundle retention + host verdict may have closed L0Ready
	// (σ-eligible). Async-fire the initial KindValue before the TPhase2a
	// backstop.
	out = append(out, maybeEarlyFire(s, e.to)...)
	return out
}

// ---- evtPhase2aFire ----------------------------------------------------

// evtPhase2aFire fires at cfg.TPhase2a. Every operator calls
// MaybeFirePhase2a, which returns one of {ValueMsg, NoValueMsg,
// Commit-NRDirect} per spec §Phase 2a. After firing, the
// afterStateDelta cascade inside MaybeFirePhase2a may have produced
// further emissions (Phase-2b Commit, upgrade ValueMsg) — capture them all.
type evtPhase2aFire struct{}

func (e *evtPhase2aFire) Describe() string { return "Phase2aFire" }

func (e *evtPhase2aFire) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	var out []desim.Scheduled
	for _, op := range s.operators {
		out = append(out, fireOnePhase2a(s, op)...)
	}
	return out
}

// ---- evtPhase2aFireOp (async-fire) -------------------------------------

// evtPhase2aFireOp fires a SINGLE op's Phase-2a emission early — at the
// moment its L_0 decision becomes determinable (L0Ready close), before
// the cluster-wide TPhase2a backstop. Scheduled by maybeEarlyFire from
// evtPhase1Arrival (retention + host verdict), evtValueMsgArrival
// (peer-reflood-V harvest), and evtLeaderFetch (leader self-observe).
// Mirrors the bare-OBFT adapter's evtCommitEmit.
type evtPhase2aFireOp struct {
	op twoab.OperatorID
}

func (e *evtPhase2aFireOp) Describe() string {
	return fmt.Sprintf("Phase2aFireOp[op=%d]", e.op)
}

func (e *evtPhase2aFireOp) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	return fireOnePhase2a(s, e.op)
}

// maybeEarlyFire schedules an early per-op Phase-2a fire if op's L0Ready
// channel has closed (σ-eligible or equivocation-observed) and the op
// hasn't already emitted. Mirrors base's maybeEarlyCommit. Returns the
// scheduled event(s) for the caller to append; the actual emission runs
// in evtPhase2aFireOp at s.Now() (this tick).
func maybeEarlyFire(s *sim, op twoab.OperatorID) []desim.Scheduled {
	if s.valueMsgEmitted[op] || s.noValueMsgEmitted[op] || s.commitEmitted[op] {
		return nil
	}
	// A byz-suppressed op's L0ReadyCh can still close (its Instance state
	// retains V + host valid independent of the DES delivery filter).
	// Skip scheduling a fire event for it — fireOnePhase2a would no-op on
	// the AllowPhase2aEmission check anyway, but without this guard we'd
	// re-schedule a dead evtPhase2aFireOp on every subsequent arrival.
	if !s.cfg.Byz.AllowPhase2aEmission(op) {
		return nil
	}
	select {
	case <-s.instances[op].L0ReadyCh():
		return []desim.Scheduled{{When: s.Now(), Ev: &evtPhase2aFireOp{op: op}}}
	default:
		return nil
	}
}

// fireOnePhase2a runs a single op's Phase-2a emission (MaybeFirePhase2a)
// and schedules the resulting ValueMsg / NoValueMsg / Commit-NRDirect
// to peers, applying byz overrides + extras. Shared by the per-op early
// fire (evtPhase2aFireOp) and the cluster-wide backstop (evtPhase2aFire).
//
// Idempotent: returns nil if the op has already emitted its Phase-2a
// message (early-fire then backstop, or vice versa). The Instance's
// phase2aFired makes MaybeFirePhase2a return the cached emission; the
// DES-side mark flags guard the propagation side so we don't double-
// broadcast.
func fireOnePhase2a(s *sim, op twoab.OperatorID) []desim.Scheduled {
	if !s.cfg.Byz.AllowPhase2aEmission(op) {
		return nil
	}
	if s.valueMsgEmitted[op] || s.noValueMsgEmitted[op] || s.commitEmitted[op] {
		return nil // already fired (early or backstop)
	}
	vm, nv, c, err := s.instances[op].MaybeFirePhase2a()
	if err != nil {
		return nil
	}
	var out []desim.Scheduled
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
	// enables Rule-6a violation scenarios that mix kinds
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
	// Phase 2a fire may have cascaded into a Phase-2b Commit emission
	// (e.g., NR-eligibility trigger already satisfied at fire time by
	// earlier-cascading peer state). The Value/NoValue case above
	// hasn't yet marked the commit as emitted; capture it here.
	out = append(out, captureCascadeEmissions(s, op)...)
	tryOpportunisticResolve(s, op)
	return out
}

// ---- evtValueMsgArrival ------------------------------------------------

type evtValueMsgArrival struct {
	from, to twoab.OperatorID
	msg      *twoab.ValueMsg
}

func (e *evtValueMsgArrival) Describe() string {
	return fmt.Sprintf("ValueMsgArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtValueMsgArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	inst := s.instances[e.to]
	_ = inst.ObserveValueMsg(e.msg)
	// Drain any host-validation requests the Instance enqueued
	// (V's first-observed via the peer-reflood-V harvest path without an
	// existing host verdict). Mirrors the OBFT adapter's drain pattern at
	// [`obft/events.go`](../obft/events.go) evtCommitArrival and the
	// production runner's drainHostValidationRequests goroutine. The
	// drain calls ApplyHostValidity, whose afterStateDelta cascade may
	// then fire the upgrade (V-drop → KindValue) — captured below.
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
	tryOpportunisticResolve(s, e.to)
	// ObserveValueMsg + drained ApplyHostValidity ran the afterStateDelta
	// cascade — capture any emissions that just fired (upgrade ValueMsg or
	// Phase-2b Commit).
	out := captureCascadeEmissions(s, e.to)
	// A harvest (+ host verdict drained above) may have
	// closed L0Ready for a V-drop receiver that just became σ-eligible.
	// Async-fire the initial KindValue. (The upgrade for an op that
	// already emitted KindNoValue goes via the cascade above, not here —
	// maybeEarlyFire no-ops once the op has emitted.)
	out = append(out, maybeEarlyFire(s, e.to)...)
	// Spec-aligned late-arrival re-resolve: if past the slot cutoff and
	// not yet decided, retry Resolve.
	if s.Now() > s.resolveDeadline {
		if _, already := s.resolved[e.to]; !already {
			out = append(out, desim.Scheduled{When: s.Now(), Ev: &evtResolveRerun{op: e.to}})
		}
	}
	return out
}

// ---- evtNoValueMsgArrival ----------------------------------------------

type evtNoValueMsgArrival struct {
	from, to twoab.OperatorID
	msg      *twoab.NoValueMsg
}

func (e *evtNoValueMsgArrival) Describe() string {
	return fmt.Sprintf("NoValueMsgArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtNoValueMsgArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	inst := s.instances[e.to]
	_ = inst.ObserveNoValueMsg(e.msg)
	tryOpportunisticResolve(s, e.to)
	out := captureCascadeEmissions(s, e.to)
	if s.Now() > s.resolveDeadline {
		if _, already := s.resolved[e.to]; !already {
			out = append(out, desim.Scheduled{When: s.Now(), Ev: &evtResolveRerun{op: e.to}})
		}
	}
	return out
}

// ---- evtCommitArrival --------------------------------------------------

type evtCommitArrival struct {
	from, to twoab.OperatorID
	commit   *twoab.Commit
}

func (e *evtCommitArrival) Describe() string {
	return fmt.Sprintf("CommitArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtCommitArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	inst := s.instances[e.to]
	_ = inst.ObserveCommit(e.commit)
	tryOpportunisticResolve(s, e.to)
	out := captureCascadeEmissions(s, e.to)
	if s.Now() > s.resolveDeadline {
		if _, already := s.resolved[e.to]; !already {
			out = append(out, desim.Scheduled{When: s.Now(), Ev: &evtResolveRerun{op: e.to}})
		}
	}
	return out
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

type evtResolveRerun struct {
	op twoab.OperatorID
}

func (e *evtResolveRerun) Describe() string {
	return fmt.Sprintf("ResolveRerun[op=%d]", e.op)
}

func (e *evtResolveRerun) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
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
	s.resolved[e.to] = &twoab.Output{
		Layer:     -1,
		Value:     append(twoab.Value{}, e.cert.Value...),
		Signature: append(twoab.Signature{}, e.cert.Signature...),
	}
	s.resolvedAt[e.to] = s.Now()
	// Cert receipt is itself the earliest "submittable" moment for this
	// op. Layer is unknown for cert-decided outputs; pass 0 so the
	// recorded time is exactly s.Now() (no walk cost).
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, e.to, 0, s.Now(), s.cfg.Epsilon3)
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
func captureCascadeEmissions(s *sim, op twoab.OperatorID) []desim.Scheduled {
	var out []desim.Scheduled
	inst := s.instances[op]

	if vm, ok := inst.OwnValueMsg(); ok && !s.valueMsgEmitted[op] {
		// Pure Phase-2a-late upgrade vs Phase-2a fire-time
		// emission: at fire-time, MaybeFirePhase2a applies the byz
		// override path explicitly. An upgrade fired from the cascade
		// here is the NoValue→Value upgrade path — apply the byz
		// upgrade-override hook.
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
func scheduleValueMsg(s *sim, from twoab.OperatorID, vm *twoab.ValueMsg) []desim.Scheduled {
	if vm == nil {
		return nil
	}
	if s.cfg.Aggregator != nil {
		recordValueMsgToAggregator(s.cfg.Aggregator, vm)
	}
	bytes := valueMsgSize(vm)
	vmCap := vm
	fromCap := from
	build := func(to twoab.OperatorID) desim.Event {
		return &evtValueMsgArrival{from: fromCap, to: to, msg: cloneValueMsg(vmCap)}
	}
	if s.cfg.Mesh != nil {
		s.emitMesh(from, ct.KindVerdict, -1, bytes, 0, s.operators, build)
		return nil
	}
	var out []desim.Scheduled
	for _, to := range s.operators {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, ct.KindVerdict) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.Rng(), from, to, ct.KindVerdict)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.Rng(), ct.OperatorID(from), ct.OperatorID(to), ct.KindVerdict)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), ct.KindVerdict, -1, bytes)
		}
		out = append(out, desim.Scheduled{
			When: s.Now() + delay,
			Ev:   &evtValueMsgArrival{from: from, to: to, msg: cloneValueMsg(vm)},
		})
	}
	return out
}

// scheduleNoValueMsg broadcasts a NoValueMsg from `from` to every other
// peer. Wire-kind: KindVerdict (shared with scheduleValueMsg) — see
// scheduleValueMsg's comment for the rationale.
func scheduleNoValueMsg(s *sim, from twoab.OperatorID, nv *twoab.NoValueMsg) []desim.Scheduled {
	if nv == nil {
		return nil
	}
	if s.cfg.Aggregator != nil {
		recordNoValueMsgToAggregator(s.cfg.Aggregator, nv)
	}
	bytes := noValueMsgSize(nv)
	nvCap := nv
	fromCap := from
	build := func(to twoab.OperatorID) desim.Event {
		return &evtNoValueMsgArrival{from: fromCap, to: to, msg: cloneNoValueMsg(nvCap)}
	}
	if s.cfg.Mesh != nil {
		s.emitMesh(from, ct.KindVerdict, -1, bytes, 0, s.operators, build)
		return nil
	}
	var out []desim.Scheduled
	for _, to := range s.operators {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, ct.KindVerdict) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.Rng(), from, to, ct.KindVerdict)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.Rng(), ct.OperatorID(from), ct.OperatorID(to), ct.KindVerdict)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), ct.KindVerdict, -1, bytes)
		}
		out = append(out, desim.Scheduled{
			When: s.Now() + delay,
			Ev:   &evtNoValueMsgArrival{from: from, to: to, msg: cloneNoValueMsg(nv)},
		})
	}
	return out
}

// scheduleCommit broadcasts a Commit from `from` to every other peer.
func scheduleCommit(s *sim, from twoab.OperatorID, c *twoab.Commit) []desim.Scheduled {
	if c == nil {
		return nil
	}
	if s.cfg.Aggregator != nil {
		recordCommitToAggregator(s.cfg.Aggregator, c)
	}
	bytes := commitSize(c)
	cCap := c
	fromCap := from
	build := func(to twoab.OperatorID) desim.Event {
		return &evtCommitArrival{from: fromCap, to: to, commit: cloneCommit(cCap)}
	}
	extraDelay := s.cfg.Byz.OverrideOwnCommitDispatchDelay(s, from)
	if s.cfg.Mesh != nil {
		s.emitMesh(from, ct.KindCommit, -1, bytes, extraDelay, s.operators, build)
		return nil
	}
	var out []desim.Scheduled
	for _, to := range s.operators {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, ct.KindCommit) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.Rng(), from, to, ct.KindCommit)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.Rng(), ct.OperatorID(from), ct.OperatorID(to), ct.KindCommit)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), ct.KindCommit, -1, bytes)
		}
		out = append(out, desim.Scheduled{
			When: s.Now() + delay + extraDelay,
			Ev:   &evtCommitArrival{from: from, to: to, commit: cloneCommit(c)},
		})
	}
	return out
}

// recordValueMsgToAggregator records per-layer σ / encrypted-claim partials
// from a ValueMsg into the offline aggregator. KindValue carries the
// emitter's σ partial on V at L_0 in L0Partial — ObserveSigma at L_0.
// L_k>0 SigmaChained LayerEntries contribute as encrypted claims.
// Credits the claimed sender's OperatorID, matching the byz-observer
// model from base OBFT.
func recordValueMsgToAggregator(agg *ct.OfflineAggregator, vm *twoab.ValueMsg) {
	from := ct.OperatorID(vm.OperatorID)
	if len(vm.L0Partial) > 0 {
		agg.ObserveSigma(from, 0, vm.V)
	}
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
// Commit into the offline aggregator. Commit is NR-side only:
//   - Side=NR: NR tag partial at L_0 → ObserveNR at L_0.
//   - Side=NRDirect: NR tag partial at L_0 + K-1 LayerEntries (the
//     NRDirect emission bundles the L_k>0 commitments with the L_0
//     emission).
//
// The σ partial rides in KindValue (handled by
// recordValueMsgToAggregator's ObserveSigma), not in Commit.
func recordCommitToAggregator(agg *ct.OfflineAggregator, c *twoab.Commit) {
	from := ct.OperatorID(c.OperatorID)
	switch c.Side {
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
// (from KindValue.L0Partial directly plus the leader's LeaderSigma) and
// L_k>0 σ-chained entries (from ValueMsg / NoValueMsg / Commit-NRDirect
// LayerEntries).
func tryOpportunisticResolve(s *sim, op twoab.OperatorID) {
	if _, already := s.vQuorumAt[op]; already {
		return
	}
	res, err := s.instances[op].Resolve()
	if err != nil {
		return
	}
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, op, res.Layer, s.Now(), s.cfg.Epsilon3)
}

// resolveOpAndBroadcastCert runs Resolve for `op`, records the outcome in
// the sim's resolved/resolveErrs maps, and (on success) schedules cert
// broadcast to peers.
//
// On success, clears any prior resolveErrs entry so the late-recovered op
// reports decided=true with no Err in the outcome.
func resolveOpAndBroadcastCert(s *sim, op twoab.OperatorID) []desim.Scheduled {
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
	decisionTime := s.Now() + time.Duration(res.Layer)*s.cfg.Epsilon3
	s.resolvedAt[op] = decisionTime
	ct.RecordFirstOpportunisticQuorum(s.vQuorumAt, op, res.Layer, s.Now(), s.cfg.Epsilon3)
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
	build := func(to twoab.OperatorID) desim.Event {
		return &evtCertArrival{from: opCap, to: to, cert: cloneCertificate(certCap)}
	}
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
