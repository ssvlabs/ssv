package obft

import (
	"container/heap"
	"errors"
	"fmt"
	mrand "math/rand"
	"slices"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftbase "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
)

// runDES is the core DES loop for OBFT. Built around obft.Instance under
// virtual time — no time.Now / time.Sleep. Single-goroutine event loop
// with a priority queue ordered by (timestamp, sequence) for tie-break
// determinism.
func runDES(cfg desConfig) (rawOutcome, error) {
	s, err := newSim(cfg)
	if err != nil {
		return rawOutcome{}, err
	}
	if err := s.start(); err != nil {
		return rawOutcome{}, err
	}
	s.runLoop()
	return s.outcome(), nil
}

type sim struct {
	cfg         desConfig
	rng         *mrand.Rand
	now         time.Duration
	queue       eventQueue
	seq         int64
	operators   []obftbase.OperatorID
	cfgObft     *obftbase.Config
	pubShares   map[obftbase.OperatorID][]byte
	clusterPub  []byte
	instances   map[obftbase.OperatorID]*obftbase.Instance
	resolved    map[obftbase.OperatorID]*obftbase.Output
	resolvedAt  map[obftbase.OperatorID]time.Duration
	resolveErrs map[obftbase.OperatorID]error
	// vQuorumAt[op] records the FIRST moment Resolve succeeded at op —
	// the earliest moment that op holds a submittable σ-cert. Driven by
	// opportunistic Resolve calls on every state-delta event
	// (evtCommitArrival, evtCertArrival), mirroring an observer-mode
	// production runner. Falls back to the schedule-anchored resolvedAt
	// time when the schedule-anchored evtResolve at RoundEndOffset is
	// what first produces quorum (since resolveOpAndBroadcastCert also
	// writes here as a fallback). Read by outcome() as Outcome.DecisionTime.
	// Plan: docs/OBFT-OPPORTUNISTIC-PHASE3-PLAN.md.
	vQuorumAt map[obftbase.OperatorID]time.Duration
	// commitEmitted[op] = true once op's KindCommit has been built and
	// dispatched. Guard for the L0Ready-driven evtCommitEmit (fired early
	// when an op's L_0 decision is determinable) vs the evtPhaseTwoStart
	// T_commit fallback (fires for any op that hasn't triggered early).
	// Without this dedup, the T_commit fallback would re-call
	// BuildOwnCommit on an already-committed instance and produce
	// ErrAlreadyCommitted.
	commitEmitted map[obftbase.OperatorID]bool
	canonValues   map[int]obftbase.Value
	trace         []ct.TraceEntry
}

func newSim(cfg desConfig) (*sim, error) {
	N := cfg.N
	operators := make([]obftbase.OperatorID, N)
	for i := 0; i < N; i++ {
		operators[i] = obftbase.OperatorID(cfg.Operators[i])
	}

	var pubShares map[obftbase.OperatorID][]byte
	var clusterPub []byte
	if cfg.BLSKeys != nil {
		pubShares = make(map[obftbase.OperatorID][]byte, N)
		for op, share := range cfg.BLSKeys.PubShares {
			pubShares[obftbase.OperatorID(op)] = share
		}
		clusterPub = cfg.BLSKeys.ClusterPubKey
	} else {
		pubShares = make(map[obftbase.OperatorID][]byte, N)
		for _, op := range operators {
			pubShares[op] = []byte{byte(op)}
		}
		clusterPub = []byte{0xCA, 0xFE}
	}

	// Per-layer candidate values sized to a realistic Electra blinded block.
	// Each layer's leader (in production) fetches an independent block from
	// their relay/beacon, so V_0..V_{K-1} are distinct candidates of similar
	// size. See ct.RealisticBlindedBlockBytes for the size derivation
	// (~5 KB SSZ-encoded SignedBlindedBeaconBlock at 8-att Electra max).
	canonValues := make(map[int]obftbase.Value, cfg.K)
	for k := 0; k < cfg.K; k++ {
		canonValues[k] = obftbase.Value(ct.MakeRealisticBlindedBlockValue(byte(k + 1)))
	}

	return &sim{
		cfg:           cfg,
		rng:           mrand.New(mrand.NewSource(cfg.Seed)),
		operators:     operators,
		pubShares:     pubShares,
		clusterPub:    clusterPub,
		canonValues:   canonValues,
		instances:     make(map[obftbase.OperatorID]*obftbase.Instance, N),
		resolved:      make(map[obftbase.OperatorID]*obftbase.Output, N),
		resolvedAt:    make(map[obftbase.OperatorID]time.Duration, N),
		resolveErrs:   make(map[obftbase.OperatorID]error, N),
		vQuorumAt:     make(map[obftbase.OperatorID]time.Duration, N),
		commitEmitted: make(map[obftbase.OperatorID]bool, N),
	}, nil
}

func (s *sim) start() error {
	K := s.cfg.K
	layers := make([]obftbase.LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = obftbase.LayerSpec{
			Leader:          s.operators[k%s.cfg.N],
			FetchAt:         s.cfg.FetchAt[k],
			BroadcastBudget: s.cfg.BroadcastBudget[k],
		}
	}
	cfgObft := &obftbase.Config{
		Height:    1,
		ClusterID: [32]byte{0x01, 0x02, 0x03},
		Operators: s.operators,
		F:         (s.cfg.N - 1) / 3,
		Layers:    layers,
		TCommit:   s.cfg.TCommit,
		Delta2:    s.cfg.Delta2,
		Eps3:      s.cfg.Epsilon3, // production obftbase.Config.Eps3 is the pure ε_3 budget
		BTT:       s.cfg.BTT,
	}
	if err := cfgObft.Validate(); err != nil {
		// Validate failures at this point mean the SimConfig translates
		// to an operating-point-incompatible obft.Config (e.g. TCommit
		// too small for the broadcast deadline at degraded BTT, deepest
		// B_{K-1} below BFT-min). Wrap with ErrConfigOutOfEnvelope so
		// the framework renders the cell as red 0% rather than logging
		// an unexpected-error warning.
		return fmt.Errorf("%w: obft adapter: invalid OBFT config: %v",
			ct.ErrConfigOutOfEnvelope, err)
	}
	s.cfgObft = cfgObft

	q := s.cfg.N - (s.cfg.N-1)/3
	useReal := s.cfg.BLSKeys != nil
	var ibe obftbase.ThresholdIBE
	if useReal {
		ibe = blsbackend.NewTLockIBE()
	} else {
		ibe = obftbase.NewStubIBE(q)
	}
	for _, op := range s.operators {
		var signer, tagSigner obftbase.Signer
		if useReal {
			share := s.cfg.BLSKeys.Shares[ct.OperatorID(op)]
			signer = blsbackend.New(share)
			tagSigner = blsbackend.NewKyberSigner(share)
		} else {
			stub := obftbase.NewStubSigner(q, []byte{byte(op)})
			signer = stub
			tagSigner = stub
		}
		inst, err := obftbase.NewInstance(cfgObft, op, signer, tagSigner, ibe, s.clusterPub, s.pubShares, nil, nil)
		if err != nil {
			return fmt.Errorf("obft adapter: new instance op %d: %w", op, err)
		}
		s.instances[op] = inst
	}

	for k := 0; k < K; k++ {
		s.schedule(s.cfg.FetchAt[k], &evtLeaderFetch{layer: k})
	}
	s.schedule(cfgObft.TCommit, &evtPhaseTwoStart{})
	s.schedule(cfgObft.RoundEndOffset(), &evtResolve{})
	s.scheduleInitialHeartbeats()
	return nil
}

func (s *sim) runLoop() {
	for s.queue.Len() > 0 {
		e := heap.Pop(&s.queue).(*queueItem)
		s.now = e.when
		if s.cfg.TraceEnabled {
			s.trace = append(s.trace, ct.TraceEntry{
				When:  e.when,
				Event: e.ev.describe(),
			})
		}
		newEvents := e.ev.handle(s)
		for _, ne := range newEvents {
			s.schedule(ne.when, ne.ev)
		}
	}
}

func (s *sim) schedule(when time.Duration, ev event) {
	s.seq++
	heap.Push(&s.queue, &queueItem{when: when, seq: s.seq, ev: ev})
}

func (s *sim) outcome() rawOutcome {
	out := rawOutcome{
		decided:       false,
		layer:         -1,
		perOp:         make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace:         s.trace,
		deadlockLayer: -1,
	}
	earliestT := time.Duration(-1)
	for _, op := range s.operators {
		o := rawOpOutcome{decided: false, layer: -1}
		if res := s.resolved[op]; res != nil {
			o.decided = true
			o.layer = res.Layer
			o.value = append([]byte{}, res.Value...)
			// Prefer the observer-mode vQuorumAt time (recorded on the
			// first state-delta that produces σ-quorum at op, BTT-
			// sensitive). Falls back to resolvedAt for the rare path
			// where decided was set without a vQuorumAt write — should
			// not happen in current code since resolveOpAndBroadcastCert
			// always writes vQuorumAt as a fallback, but the fallback
			// here keeps the metric safely defined.
			if t, ok := s.vQuorumAt[op]; ok {
				o.time = t
			} else {
				o.time = s.resolvedAt[op]
			}
			if earliestT < 0 || o.time < earliestT {
				earliestT = o.time
				out.decided = true
				out.layer = res.Layer
				out.value = append([]byte(nil), res.Value...)
				out.decisionTime = o.time
			}
		}
		if err, ok := s.resolveErrs[op]; ok {
			o.err = err.Error()
			// Capture the deepest deadlock layer across non-decided
			// ops. errors.As walks the Unwrap chain so the *ResolveError
			// surfaces regardless of any future fmt.Errorf wrapping.
			if !o.decided {
				var rerr *obftbase.ResolveError
				if errors.As(err, &rerr) && rerr.Reason == obftbase.ResolveFailureDeadlock {
					if rerr.StoppedAtLayer > out.deadlockLayer {
						out.deadlockLayer = rerr.StoppedAtLayer
					}
				}
			}
		}
		o.evidenceByRule = evidenceByRule(s.instances[op].Evidence())
		out.perOp[ct.OperatorID(op)] = o
	}
	return out
}

func (s *sim) honestLeaderValue(layer int) obftbase.Value {
	return s.canonValues[layer]
}

// emitToAll schedules per-receiver arrival events for `from`'s message,
// honoring byz-pattern delivery / delay overrides. `bytes` is the wire size
// of the message (for bandwidth accounting; pass 0 to skip). `layer` is the
// OBFT layer for per-layer bandwidth accounting (use -1 for layer-agnostic).
// `extraDelay` is added on top of the per-pair network delay — used by byz
// patterns to push a specific operator's own-emission past a protocol
// deadline (e.g. OverrideOwnCommitDispatchDelay for late KindCommit
// scenarios).
//
// Transport dispatch:
//   - cfg.Mesh == nil (DeliveryDirect): per-recipient fanout against
//     cfg.Network with per-(from, to) byz checks. The catalog default.
//   - cfg.Mesh != nil (DeliveryMesh): publish to `from`'s mesh neighbors
//     (cluster ops + relays). Byz primitives (AllowDelivery, OverrideDelay)
//     apply only at the publish step — re-flooding from neighbors still
//     delivers to wire-suppressed receivers one hop later. Adversarial
//     scenarios should stay on DeliveryDirect; see Scenario.Delivery.
func (s *sim) emitToAll(from obftbase.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, build func(to obftbase.OperatorID) event) {
	if s.cfg.Mesh != nil {
		s.emitMesh(from, kind, layer, bytes, extraDelay, s.operators, build)
		return
	}
	s.emitDirect(from, kind, layer, bytes, extraDelay, s.operators, build)
}

// emitDirect is the original full-fanout transport path, factored out so
// evtLeaderFetch's selective-recipients loop and emitToAll share one
// implementation. `recipients` filters the cluster down to a chosen subset
// (used by byz patterns that emit to less than the full cluster); pass
// s.operators for the "all cluster ops" default.
func (s *sim) emitDirect(from obftbase.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, recipients []obftbase.OperatorID, build func(to obftbase.OperatorID) event) {
	for _, to := range recipients {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, kind) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.rng, from, to, kind)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, ct.OperatorID(from), ct.OperatorID(to), kind)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), kind, layer, bytes)
		}
		ev := build(to)
		s.schedule(s.now+delay+extraDelay, ev)
	}
}

// emitMesh publishes `from`'s message through the per-sim MeshTopology.
// First-hop arrivals go to `from`'s mesh neighbors (cluster ops + relays);
// each evtMeshArrival's handler dedup'd-forwards onward. `recipients` is
// retained for parity with emitDirect's signature but is consulted only
// to apply per-(from, to) byz primitives at the publish step (byz patterns
// that target a specific subset of cluster ops still suppress emission to
// those direct receivers — but a re-flooding neighbor will deliver a
// hop later, which is the libp2p-true behavior).
func (s *sim) emitMesh(from obftbase.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, recipients []obftbase.OperatorID, build func(to obftbase.OperatorID) event) {
	mesh := s.cfg.Mesh
	fromOp := ct.OperatorID(from)
	fromNode := mesh.NodeForOperator(fromOp)
	// Self-mark so a forwarded copy reflooding back to us is dedup'd
	// rather than re-delivered.
	id := mesh.NewMsgID()
	mesh.MarkSeen(fromNode, id)
	// Stash a reinject closure on the publisher's mcache so it can
	// answer IWANT for this mid until eviction. No-op when gossip is
	// disabled.
	s.cacheArrivalForGossip(fromNode, fromNode, id, kind, layer, bytes, build)
	fromEP := mesh.EndpointFor(fromNode)
	for _, neighbor := range mesh.Neighbors(fromNode) {
		// Per-(from, to) byz primitives at publish only (relays escape
		// these checks since they have no cluster identity).
		isProto := mesh.IsProtocol(neighbor)
		if isProto {
			toOp := obftbase.OperatorID(mesh.OperatorForNode(neighbor))
			// Linear scan of recipients (n ≤ 13 in SSV) beats the
			// allocate-then-lookup of a hash set for this filter.
			if !slices.Contains(recipients, toOp) {
				continue
			}
			if !s.cfg.Byz.AllowDelivery(from, toOp, kind) {
				continue
			}
		}
		// Mesh hops use the mesh's HopDelay model, not the cluster-wide
		// Network model — that's the calibration story. (OverrideDelay
		// is byz-pattern-specific to direct fanout and does not apply.)
		// Endpoint IDs from EndpointFor give stateful NetworkModel impls
		// a unique key per mesh edge (relays get synthetic IDs ≥
		// RelayEndpointBase, distinct from cluster ops 1..N).
		delay := mesh.SampleHopDelay(s.rng, fromEP, mesh.EndpointFor(neighbor), kind)
		mesh.RecordMeshHop(s.cfg.Bandwidth, fromNode, neighbor, kind, layer, bytes)
		s.schedule(s.now+delay+extraDelay, &evtMeshArrival{
			from:      fromNode,
			to:        neighbor,
			publisher: fromNode,
			msgID:     id,
			kind:      kind,
			layer:     layer,
			bytes:     bytes,
			builder:   build,
		})
	}
}

func (s *sim) observedOffset() time.Duration { return s.now }

// cacheArrivalForGossip stashes a reinject closure on `cacheOwner`'s
// mcache so it can answer IWANT for `msgID`. Called by emitMesh (at
// the publisher, cacheOwner == publisher) and by evtMeshArrival.handle
// (at each receiver, cacheOwner = receiver, publisher carried over).
// No-op when gossip is disabled — the closure-allocation cost is
// avoided entirely on the eager-only path.
//
// The closure preserves `publisher` and the full message payload so
// the rebuilt evtMeshArrival on IWANT-response looks indistinguishable
// from a fresh eager-mesh hop to downstream consumers (same builder,
// same msgID, same publisher metadata for the publisher-skip).
func (s *sim) cacheArrivalForGossip(
	cacheOwner, publisher ct.MeshNode,
	msgID ct.MsgID, kind ct.MsgKind, layer int, bytes int64,
	builder func(to obftbase.OperatorID) event,
) {
	mesh := s.cfg.Mesh
	g := mesh.Gossip()
	if !g.Enabled {
		return
	}
	mesh.MCacheInsert(cacheOwner, msgID, ct.MCacheEntry{
		Kind:  kind,
		Bytes: bytes,
		Reinject: func(requester ct.MeshNode) {
			respEP := mesh.EndpointFor(cacheOwner)
			reqEP := mesh.EndpointFor(requester)
			delay := s.cfg.Network.Delay(s.rng, respEP, reqEP, kind)
			mesh.RecordMeshHop(s.cfg.Bandwidth, cacheOwner, requester, kind, layer, bytes)
			s.schedule(s.now+delay, &evtMeshArrival{
				from:      cacheOwner,
				to:        requester,
				publisher: publisher,
				msgID:     msgID,
				kind:      kind,
				layer:     layer,
				bytes:     bytes,
				builder:   builder,
			})
		},
	}, g.HistoryLength)
}

// scheduleInitialHeartbeats fires evtMeshHeartbeat events across every
// mesh node when the gossip layer is enabled, anchored at sim time 0.
// Per-node phase offset = (node × HeartbeatInterval / TotalNodes)
// staggers the cluster's heartbeats so they don't all fire at
// t = k·HeartbeatInterval. Ticks beyond RelayCutoff are not scheduled
// — past the submit deadline, the slot's decision is moot, so further
// gossip cadence buys nothing. No-op when gossip is disabled or
// DeliveryDirect is in effect.
//
// Pre-scheduling (rather than self-rescheduling inside the handler)
// keeps the event queue finite-by-construction: a misconfigured
// scenario without a decision-triggering chain still terminates when
// the queue drains.
func (s *sim) scheduleInitialHeartbeats() {
	mesh := s.cfg.Mesh
	if mesh == nil {
		return
	}
	g := mesh.Gossip()
	// HeartbeatInterval ≤ 0 is unreachable via the normal construction
	// path (WithDefaults fills 0 → 700ms; MeshTopology snapshots that
	// at build time), but guarding here makes the inner `tick +=`
	// loop safe against a hand-mutated config that bypassed defaults.
	if !g.Enabled || g.HeartbeatInterval <= 0 {
		return
	}
	total := mesh.TotalNodes()
	if total <= 0 {
		return
	}
	phase := g.HeartbeatInterval / time.Duration(total)
	for i := 0; i < total; i++ {
		nodeOffset := time.Duration(i) * phase
		for tick := time.Duration(0); ; tick += g.HeartbeatInterval {
			at := nodeOffset + tick
			if at > s.cfg.RelayCutoff {
				break
			}
			s.schedule(at, &evtMeshHeartbeat{node: ct.MeshNode(i)})
		}
	}
}
