package obft

import (
	"container/heap"
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
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
			if s.cfg.Bandwidth != nil {
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

func (s *sim) forgeByzBundle(leader obft.OperatorID, layer int, v obft.Value) *obft.Phase1Bundle {
	var signer obft.Signer
	if s.cfg.BLSKeys != nil {
		share := s.cfg.BLSKeys.Shares[ct.OperatorID(leader)]
		signer = blsbackend.New(share)
	} else {
		q := s.cfgObft.QV()
		signer = obft.NewStubSigner(q, []byte{byte(leader)})
	}
	sig, err := signer.SignPartial(v)
	if err != nil {
		panic(fmt.Sprintf("obft adapter: forge bundle for leader %d: %v", leader, err))
	}
	return &obft.Phase1Bundle{
		ClusterID:  s.cfgObft.ClusterID,
		OperatorID: leader,
		Height:     s.cfgObft.Height,
		Layer:      layer,
		Value:      append(obft.Value{}, v...),
		SigmaV:     sig,
	}
}

// ---- evtPhase1Arrival --------------------------------------------------

type evtPhase1Arrival struct {
	from, to obft.OperatorID
	layer    int
	bundle   *obft.Phase1Bundle
}

func (e *evtPhase1Arrival) describe() string {
	return fmt.Sprintf("Phase1Arrival[from=%d to=%d layer=%d v=%s]",
		e.from, e.to, e.layer, hashValue(e.bundle.Value))
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
		s.emitToAll(op, ct.KindCommit, -1, commitSize(c), func(to obft.OperatorID) event {
			return &evtCommitArrival{from: op, to: to, commit: cloneCommit(c)}
		})
		// Byz patterns may add ADDITIONAL commits (e.g. cross-onion
		// equivocation emits two structurally-distinct Commits).
		for _, extra := range s.cfg.Byz.BuildExtraCommits(s, op, c) {
			extra := extra
			if s.cfg.Aggregator != nil {
				recordCommitToAggregator(s.cfg.Aggregator, extra)
			}
			s.emitToAll(op, ct.KindCommit, -1, commitSize(extra), func(to obft.OperatorID) event {
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
func recordCommitToAggregator(agg *ct.OfflineAggregator, c *obft.Commit) {
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
		agg.ObserveSigma(ct.OperatorID(w.Leader), w.Layer, w.Value)
	}
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
		if !s.cfg.Byz.AllowCertificateBroadcast(op) {
			continue
		}
		cert, err := s.instances[op].BuildCertificate(res)
		if err != nil || cert == nil {
			continue
		}
		certBytes := certSize(cert)
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
			if s.cfg.Bandwidth != nil {
				s.cfg.Bandwidth.Emission(ct.OperatorID(op), ct.OperatorID(to),
					ct.KindCertificate, -1, certBytes)
			}
			out = append(out, scheduledEvent{
				when: s.now + delay,
				ev:   &evtCertArrival{from: op, to: to, cert: cloneCertificate(cert)},
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
	if _, already := s.resolved[e.to]; already {
		return nil
	}
	s.resolved[e.to] = &obft.Output{
		Layer:     -1,
		Value:     append(obft.Value{}, e.cert.Value...),
		Signature: append(obft.Signature{}, e.cert.Signature...),
	}
	s.resolvedAt[e.to] = s.now
	// Cert-gossip rescue supersedes a prior local-resolve failure; clear the
	// stale error so outcome() doesn't report decided=true with a non-empty
	// Err (otherwise confuses callers reading the per-op outcome).
	delete(s.resolveErrs, e.to)
	return nil
}
