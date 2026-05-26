package obft

import (
	"errors"
	"fmt"
	"slices"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/desim"
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
	s.Run(s)
	return s.outcome(), nil
}

type sim struct {
	*desim.Engine
	cfg         desConfig
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
	// See docs/OBFT.md §Phase 3 (opportunistic Resolve).
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
	// crashed[op] = true for completely-offline operators. Wire behavior is
	// suppressed by crashOverlay; this set lets outcome() exclude them from
	// the decided set and stamp Err="offline".
	crashed map[obftbase.OperatorID]bool
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

	crashed := make(map[obftbase.OperatorID]bool, len(cfg.Crashed))
	for _, op := range cfg.Crashed {
		crashed[obftbase.OperatorID(op)] = true
	}

	return &sim{
		Engine:        desim.NewEngine(cfg.Seed, cfg.TraceEnabled),
		cfg:           cfg,
		operators:     operators,
		pubShares:     pubShares,
		clusterPub:    clusterPub,
		canonValues:   canonValues,
		crashed:       crashed,
		instances:     make(map[obftbase.OperatorID]*obftbase.Instance, N),
		resolved:      make(map[obftbase.OperatorID]*obftbase.Output, N),
		resolvedAt:    make(map[obftbase.OperatorID]time.Duration, N),
		resolveErrs:   make(map[obftbase.OperatorID]error, N),
		vQuorumAt:     make(map[obftbase.OperatorID]time.Duration, N),
		commitEmitted: make(map[obftbase.OperatorID]bool, N),
	}, nil
}

// Mesh / Network / Bandwidth satisfy desim.Host (Now/Rng/Schedule/Run/Trace
// come from the embedded *desim.Engine).
func (s *sim) Mesh() *ct.MeshTopology         { return s.cfg.Mesh }
func (s *sim) Network() ct.NetworkModel       { return s.cfg.Network }
func (s *sim) Bandwidth() *ct.BandwidthReport { return s.cfg.Bandwidth }

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
		s.Schedule(s.cfg.FetchAt[k], &evtLeaderFetch{layer: k})
	}
	s.Schedule(cfgObft.TCommit, &evtPhaseTwoStart{})
	s.Schedule(cfgObft.RoundEndOffset(), &evtResolve{})
	desim.ScheduleInitialHeartbeats(s, s.cfg.RelayCutoff)
	return nil
}

func (s *sim) outcome() rawOutcome {
	out := rawOutcome{
		decided:       false,
		layer:         -1,
		perOp:         make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace:         s.Trace(),
		deadlockLayer: -1,
	}
	earliestT := time.Duration(-1)
	for _, op := range s.operators {
		if s.crashed[op] {
			// Completely offline: never a decider, reported with Err="offline"
			// so Terminated stays satisfied (Decided=false, Err!="") and the
			// op is excluded from SingleV / HonestAgreement (decided-only).
			out.perOp[ct.OperatorID(op)] = rawOpOutcome{decided: false, layer: -1, err: "offline"}
			continue
		}
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
		// Snapshot the most-recent Resolve walk's per-layer trace. The
		// trace is overwritten on every Resolve call; capturing here
		// reflects the LAST Resolve fired for this op during the sim —
		// which is the one corresponding to the final outcome state
		// (decision time for deciders, last failed attempt for misses).
		// Bounded by K (≤ N at the production K=N convention) so cost
		// is negligible.
		//
		// LOAD-BEARING for the bucket-3 D1 check in safety.go: the
		// trace captured here must reflect the SAME Resolve walk that
		// produced the o.decided / o.round fields above (set from
		// s.resolved[op] which is written by resolveOpAndBroadcastCert).
		// Today this holds because the event-scheduling guards in
		// events.go (s.vQuorumAt-guard on tryOpportunisticResolve,
		// s.resolved-guard on evtResolveRerun) ensure the last
		// Resolve() invocation is the one that set s.resolved[op]. A
		// future refactor that decouples them (e.g., drops the
		// s.resolved-guard on evtResolveRerun, or adds an
		// opportunistic post-evtResolve Resolve to refresh metrics)
		// would produce false-positive D1 case-(b) violations (trace
		// from a later, shallower-σ walk vs. Round from the first,
		// deeper-σ walk). If that refactor lands, either capture the
		// trace snapshot at the s.resolved[op] write-site instead, or
		// re-derive D1's case-(b) logic.
		if trace := s.instances[op].LastResolveLayerAttempts(); len(trace) > 0 {
			o.resolveLayerAttempts = append([]obftbase.LayerAttempt(nil), trace...)
		}
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
func (s *sim) emitToAll(from obftbase.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, build func(to obftbase.OperatorID) desim.Event) {
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
func (s *sim) emitDirect(from obftbase.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, recipients []obftbase.OperatorID, build func(to obftbase.OperatorID) desim.Event) {
	for _, to := range recipients {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, kind) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.Rng(), from, to, kind)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.Rng(), ct.OperatorID(from), ct.OperatorID(to), kind)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), kind, layer, bytes)
		}
		ev := build(to)
		s.Schedule(s.Now()+delay+extraDelay, ev)
	}
}

// emitMesh publishes `from`'s message through the per-sim MeshTopology.
// First-hop arrivals go to `from`'s mesh neighbors (cluster ops + relays);
// the shared desim.MeshArrival handler dedup'd-forwards onward. `recipients` is
// retained for parity with emitDirect's signature but is consulted only
// to apply per-(from, to) byz primitives at the publish step (byz patterns
// that target a specific subset of cluster ops still suppress emission to
// those direct receivers — but a re-flooding neighbor will deliver a
// hop later, which is the libp2p-true behavior).
func (s *sim) emitMesh(from obftbase.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, recipients []obftbase.OperatorID, build func(to obftbase.OperatorID) desim.Event) {
	mesh := s.cfg.Mesh
	fromOp := ct.OperatorID(from)
	fromNode := mesh.NodeForOperator(fromOp)
	// Self-mark so a forwarded copy reflooding back to us is dedup'd
	// rather than re-delivered.
	id := mesh.NewMsgID()
	mesh.MarkSeen(fromNode, id)
	// Wrap the native-typed builder to the shared transport's ct.OperatorID
	// signature; the obftbase↔ct conversion lands here, at the single
	// mesh-publish boundary.
	meshBuild := func(to ct.OperatorID) desim.Event { return build(obftbase.OperatorID(to)) }
	// Stash a reinject closure on the publisher's mcache so it can
	// answer IWANT for this mid until eviction. No-op when gossip is
	// disabled.
	desim.CacheArrivalForGossip(s, fromNode, fromNode, id, kind, layer, bytes, meshBuild)
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
		delay := mesh.SampleHopDelay(s.Rng(), fromEP, mesh.EndpointFor(neighbor), kind)
		mesh.RecordMeshHop(s.cfg.Bandwidth, fromNode, neighbor, kind, layer, bytes)
		s.Schedule(s.Now()+delay+extraDelay, &desim.MeshArrival{
			From:      fromNode,
			To:        neighbor,
			Publisher: fromNode,
			MsgID:     id,
			Kind:      kind,
			Layer:     layer,
			Bytes:     bytes,
			Builder:   meshBuild,
		})
	}
}

func (s *sim) observedOffset() time.Duration { return s.Now() }
