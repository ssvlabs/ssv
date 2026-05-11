package obft

import (
	"container/heap"
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftbase "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
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

// ---- evtLeaderFetch ----------------------------------------------------

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
		ClusterID:  s.cfgObft.ClusterID,
		OperatorID: leader,
		Height:     s.cfgObft.Height,
		Layer:      layer,
		Value:      append(obftbase.Value{}, v...),
		SigmaV:     sig,
	}
}

// ---- evtPhase1Arrival --------------------------------------------------

type evtPhase1Arrival struct {
	from, to obftbase.OperatorID
	layer    int
	bundle   *obftbase.Phase1Bundle
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

// ---- evtPhaseTwoStart --------------------------------------------------

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
		c = s.cfg.Byz.OverrideCommit(s, op, c)
		// OfflineAggregator: op's Commit hits the wire — record per-layer
		// σ side (plaintext at L_0, encrypted-claim at L_k>0) and per-NR.
		if s.cfg.Aggregator != nil {
			recordCommitToAggregator(s.cfg.Aggregator, c)
		}
		// Byz patterns may delay their own KindCommit dispatch to land past
		// RoundEndOffset, exercising the spec §Phase 3 late-arrival re-resolve
		// path (gated by cfg.EnableLateCommitRerun).
		extraDelay := s.cfg.Byz.OverrideOwnCommitDispatchDelay(s, op)
		s.emitToAll(op, ct.KindCommit, -1, commitSize(c), extraDelay, func(to obftbase.OperatorID) event {
			return &evtCommitArrival{from: op, to: to, commit: cloneCommit(c)}
		})
		// Byz patterns may add ADDITIONAL commits (e.g. cross-onion
		// equivocation emits two structurally-distinct Commits).
		for _, extra := range s.cfg.Byz.BuildExtraCommits(s, op, c) {
			extra := extra
			if s.cfg.Aggregator != nil {
				recordCommitToAggregator(s.cfg.Aggregator, extra)
			}
			s.emitToAll(op, ct.KindCommit, -1, commitSize(extra), extraDelay, func(to obftbase.OperatorID) event {
				return &evtCommitArrival{from: op, to: to, commit: cloneCommit(extra)}
			})
		}
	}
	return nil
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

func (e *evtCommitArrival) describe() string {
	return fmt.Sprintf("CommitArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtCommitArrival) handle(s *sim) []scheduledEvent {
	_ = s.instances[e.to].ObserveCommit(e.commit)

	// Spec §Phase 3 / "Re-running on late KindCommit arrivals": if this commit
	// landed past RoundEndOffset, the receiver may re-run Resolve to
	// incorporate the new partial. Skip if the receiver already decided.
	if !s.cfg.EnableLateCommitRerun {
		return nil
	}
	if s.now <= s.cfgObft.RoundEndOffset() {
		return nil
	}
	if _, already := s.resolved[e.to]; already {
		return nil
	}
	// Schedule the re-resolve immediately at the current sim time so the new
	// state is incorporated before any further events fire at this timestamp.
	return []scheduledEvent{{when: s.now, ev: &evtResolveRerun{op: e.to}}}
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

func (e *evtResolveRerun) describe() string {
	return fmt.Sprintf("ResolveRerun[op=%d]", e.op)
}

func (e *evtResolveRerun) handle(s *sim) []scheduledEvent {
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
func resolveOpAndBroadcastCert(s *sim, op obftbase.OperatorID) []scheduledEvent {
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
	decisionTime := s.now + time.Duration(res.Layer)*s.cfg.Epsilon3
	s.resolvedAt[op] = decisionTime
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
		// Cert is broadcast immediately after this op's local decision,
		// not at evtResolve's fire time. Important when the per-layer walk
		// cost shifted decisionTime past s.now.
		out = append(out, scheduledEvent{
			when: decisionTime + delay,
			ev:   &evtCertArrival{from: op, to: to, cert: cloneCertificate(cert)},
		})
	}
	return out
}

// ---- evtCertArrival ----------------------------------------------------

type evtCertArrival struct {
	from, to obftbase.OperatorID
	cert     *obftbase.Certificate
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
	s.resolved[e.to] = &obftbase.Output{
		Layer:     -1,
		Value:     append(obftbase.Value{}, e.cert.Value...),
		Signature: append(obftbase.Signature{}, e.cert.Signature...),
	}
	s.resolvedAt[e.to] = s.now
	// Cert-gossip rescue supersedes a prior local-resolve failure; clear the
	// stale error so outcome() doesn't report decided=true with a non-empty
	// Err (otherwise confuses callers reading the per-op outcome).
	delete(s.resolveErrs, e.to)
	return nil
}
