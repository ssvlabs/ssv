package twoab

import (
	"container/heap"
	"fmt"
	mrand "math/rand"
	"slices"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// runDES is the core DES loop for 2abOBFT. Built around twoab.Instance under
// virtual time — no time.Now / time.Sleep. Single-goroutine event loop
// with a priority queue ordered by (timestamp, sequence) for tie-break
// determinism.
//
// Mirrors the bare OBFT adapter's DES shape (consensustest/obft/des.go);
// the 2ab-specific delta is the Phase-2a verdict-broadcast event pair
// scheduled between leader-fetch and Phase-2b emission.
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
	operators   []twoab.OperatorID
	cfgTwoab    *twoab.Config
	pubShares   map[twoab.OperatorID][]byte
	clusterPub  []byte
	instances   map[twoab.OperatorID]*twoab.Instance
	resolved    map[twoab.OperatorID]*twoab.Output
	resolvedAt  map[twoab.OperatorID]time.Duration
	resolveErrs map[twoab.OperatorID]error
	// vQuorumAt[op] records the FIRST moment Resolve succeeded at op —
	// the earliest moment that op holds a submittable σ-cert. Driven by
	// opportunistic Resolve calls on every state-delta event
	// (evtOnion2bArrival, evtCertArrival), mirroring an observer-mode
	// production runner. Falls back to the schedule-anchored resolvedAt
	// time when the schedule-anchored evtResolve at RoundEndOffset is
	// what first produces quorum (resolveOpAndBroadcastCert also writes
	// here as a fallback). Read by outcome() as Outcome.DecisionTime.
	// Plan: docs/OBFT-OPPORTUNISTIC-PHASE3-PLAN.md.
	vQuorumAt   map[twoab.OperatorID]time.Duration
	canonValues map[int]twoab.Value
	trace       []ct.TraceEntry
}

func newSim(cfg desConfig) (*sim, error) {
	N := cfg.N
	operators := make([]twoab.OperatorID, N)
	for i := 0; i < N; i++ {
		operators[i] = twoab.OperatorID(cfg.Operators[i])
	}

	var pubShares map[twoab.OperatorID][]byte
	var clusterPub []byte
	if cfg.BLSKeys != nil {
		pubShares = make(map[twoab.OperatorID][]byte, N)
		for op, share := range cfg.BLSKeys.PubShares {
			pubShares[twoab.OperatorID(op)] = share
		}
		clusterPub = cfg.BLSKeys.ClusterPubKey
	} else {
		pubShares = make(map[twoab.OperatorID][]byte, N)
		for _, op := range operators {
			pubShares[op] = []byte{byte(op)}
		}
		clusterPub = []byte{0xCA, 0xFE}
	}

	// Per-layer candidate values sized to a realistic Electra blinded block
	// (~5 KB) — same convention as the OBFT adapter so bandwidth comparisons
	// land on apples-to-apples footing.
	canonValues := make(map[int]twoab.Value, cfg.K)
	for k := 0; k < cfg.K; k++ {
		canonValues[k] = twoab.Value(ct.MakeRealisticBlindedBlockValue(byte(k + 1)))
	}

	return &sim{
		cfg:         cfg,
		rng:         mrand.New(mrand.NewSource(cfg.Seed)),
		operators:   operators,
		pubShares:   pubShares,
		clusterPub:  clusterPub,
		canonValues: canonValues,
		instances:   make(map[twoab.OperatorID]*twoab.Instance, N),
		resolved:    make(map[twoab.OperatorID]*twoab.Output, N),
		resolvedAt:  make(map[twoab.OperatorID]time.Duration, N),
		resolveErrs: make(map[twoab.OperatorID]error, N),
		vQuorumAt:   make(map[twoab.OperatorID]time.Duration, N),
	}, nil
}

func (s *sim) start() error {
	K := s.cfg.K
	layers := make([]twoab.LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = twoab.LayerSpec{
			Leader:          s.operators[k%s.cfg.N],
			FetchAt:         s.cfg.FetchAt[k],
			BroadcastBudget: s.cfg.BroadcastBudget[k],
		}
	}
	cfgTwoab := &twoab.Config{
		Height:    1,
		ClusterID: [32]byte{0x01, 0x02, 0x03},
		Operators: s.operators,
		F:         (s.cfg.N - 1) / 3,
		Layers:    layers,
		TCommit:   s.cfg.TCommit,
		Delta2a:   s.cfg.Delta2a,
		Delta2b:   s.cfg.Delta2b,
		Delta3:    s.cfg.Epsilon3,
		BTT:       s.cfg.BTT,
	}
	if err := cfgTwoab.Validate(); err != nil {
		// Validate failures at this point mean the SimConfig translates
		// to an operating-point-incompatible twoab.Config (e.g. TCommit
		// non-positive at high BTT, deepest B_{K-1} below BFT-min).
		// Wrap with ErrConfigOutOfEnvelope so the framework renders the
		// cell as red 0% rather than logging an unexpected-error warning.
		return fmt.Errorf("%w: twoab adapter: invalid 2ab config: %v",
			ct.ErrConfigOutOfEnvelope, err)
	}
	s.cfgTwoab = cfgTwoab

	q := s.cfg.N - (s.cfg.N-1)/3
	useReal := s.cfg.BLSKeys != nil
	var ibe obft.ThresholdIBE
	if useReal {
		ibe = blsbackend.NewTLockIBE()
	} else {
		ibe = obft.NewStubIBE(q)
	}
	for _, op := range s.operators {
		var signer, tagSigner obft.Signer
		if useReal {
			share := s.cfg.BLSKeys.Shares[ct.OperatorID(op)]
			signer = blsbackend.New(share)
			tagSigner = blsbackend.NewKyberSigner(share)
		} else {
			stub := obft.NewStubSigner(q, []byte{byte(op)})
			signer = stub
			tagSigner = stub
		}
		inst, err := twoab.NewInstance(cfgTwoab, op, signer, tagSigner, ibe, s.clusterPub, s.pubShares, nil, nil)
		if err != nil {
			return fmt.Errorf("twoab adapter: new instance op %d: %w", op, err)
		}
		s.instances[op] = inst
	}

	for k := 0; k < K; k++ {
		s.schedule(s.cfg.FetchAt[k], &evtLeaderFetch{layer: k})
	}
	// Phase-2a verdict-broadcast window starts at T_verdict_start; each
	// honest op emits their verdict at T_verdict_max - ε_proc per spec
	// §Phase 2a. ε_proc is small (a few ms); we use Epsilon3 as a stand-in
	// here (same scale).
	s.schedule(cfgTwoab.TVerdictMax()-s.cfg.Epsilon3, &evtVerdictBroadcastStart{})
	s.schedule(cfgTwoab.TCommit, &evtPhaseTwoBStart{})
	s.schedule(cfgTwoab.RoundEndOffset(), &evtResolve{})
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
		decided: false,
		layer:   -1,
		perOp:   make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace:   s.trace,
	}
	earliestT := time.Duration(-1)
	for _, op := range s.operators {
		o := rawOpOutcome{decided: false, layer: -1}
		if res := s.resolved[op]; res != nil {
			o.decided = true
			o.layer = res.Layer
			o.value = append([]byte{}, res.Value...)
			// Prefer the observer-mode vQuorumAt time (recorded on the
			// first state-delta that produces σ-quorum at op,
			// BTT-sensitive); resolveOpAndBroadcastCert writes vQuorumAt
			// as a fallback for the schedule-anchored path, so this
			// reads uniformly.
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
		}
		o.evidenceByRule = evidenceByRule(s.instances[op].Evidence())
		out.perOp[ct.OperatorID(op)] = o
	}
	return out
}

func (s *sim) honestLeaderValue(layer int) twoab.Value {
	return s.canonValues[layer]
}

// emitToAll schedules per-receiver arrival events for `from`'s message,
// honoring byz-pattern delivery / delay overrides. `bytes` is the wire size
// of the message (for bandwidth accounting; pass 0 to skip). `layer` is the
// 2ab layer for per-layer bandwidth accounting (use -1 for layer-agnostic).
// `extraDelay` is added on top of the per-pair network delay — used by byz
// patterns to push a specific operator's own-emission past a protocol
// deadline.
//
// Transport dispatch mirrors the OBFT adapter: DeliveryDirect (cfg.Mesh ==
// nil) keeps the original full-fanout path; DeliveryMesh routes through
// the per-sim MeshTopology with dedup + reflood. See OBFT emitToAll for
// the byz-primitive caveats under mesh mode.
func (s *sim) emitToAll(from twoab.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, build func(to twoab.OperatorID) event) {
	if s.cfg.Mesh != nil {
		s.emitMesh(from, kind, layer, bytes, extraDelay, s.operators, build)
		return
	}
	s.emitDirect(from, kind, layer, bytes, extraDelay, s.operators, build)
}

// emitDirect is the original full-fanout transport path, factored out so
// evtLeaderFetch's selective-recipients loop and resolveOpAndBroadcastCert's
// cert loop share one implementation with emitToAll.
func (s *sim) emitDirect(from twoab.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, recipients []twoab.OperatorID, build func(to twoab.OperatorID) event) {
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

// emitMesh publishes via the per-sim MeshTopology. Behavior parallels the
// OBFT adapter's emitMesh; see that file for the byz / bandwidth /
// recipient-filter caveats.
func (s *sim) emitMesh(from twoab.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, recipients []twoab.OperatorID, build func(to twoab.OperatorID) event) {
	mesh := s.cfg.Mesh
	fromOp := ct.OperatorID(from)
	fromNode := mesh.NodeForOperator(fromOp)
	id := mesh.NewMsgID()
	mesh.MarkSeen(fromNode, id)
	fromEP := mesh.EndpointFor(fromNode)
	for _, neighbor := range mesh.Neighbors(fromNode) {
		isProto := mesh.IsProtocol(neighbor)
		if isProto {
			toOp := twoab.OperatorID(mesh.OperatorForNode(neighbor))
			// Linear scan is faster than a hash-set allocate at n ≤ 13.
			if !slices.Contains(recipients, toOp) {
				continue
			}
			if !s.cfg.Byz.AllowDelivery(from, toOp, kind) {
				continue
			}
		}
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
