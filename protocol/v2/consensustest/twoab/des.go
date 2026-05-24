package twoab

import (
	"errors"
	"fmt"
	"slices"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/desim"
	"github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// runDES is the core DES loop for 2abOBFT. Built around twoab.Instance under
// virtual time — no time.Now / time.Sleep. Single-goroutine event loop
// with a priority queue ordered by (timestamp, sequence) for tie-break
// determinism.
//
// Phase 2b is dynamic in 2abOBFT: commits fire via the per-tick
// afterStateDelta cascade inside Observe* / MaybeFirePhase2a /
// ApplyHostValidity, NOT at a scheduled phase-2b-start event. The DES
// captures these cascade emissions on every event boundary via
// captureCascadeEmissions and schedules per-recipient arrivals dynamically.
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
	operators   []twoab.OperatorID
	cfgTwoab    *twoab.Config
	pubShares   map[twoab.OperatorID][]byte
	clusterPub  []byte
	instances   map[twoab.OperatorID]*twoab.Instance
	signers     map[twoab.OperatorID]obft.Signer // retained for byz-forging paths that need direct signer access bypassing EKM
	resolved    map[twoab.OperatorID]*twoab.Output
	resolvedAt  map[twoab.OperatorID]time.Duration
	resolveErrs map[twoab.OperatorID]error

	// vQuorumAt[op] records the FIRST moment Resolve succeeded at op —
	// the earliest moment that op holds a submittable σ-cert. Driven by
	// opportunistic Resolve calls on every state-delta event
	// (Phase2aFire, ValueMsgArrival, NoValueMsgArrival, CommitArrival,
	// CertArrival), mirroring an observer-mode production runner. Falls
	// back to the schedule-anchored resolvedAt time when the
	// schedule-anchored evtResolve at slot deadline is what first
	// produces quorum (resolveOpAndBroadcastCert also writes here as a
	// fallback). Read by outcome() as Outcome.DecisionTime. See
	// docs/2abOBFT.md §Phase 3 (reconstruction — observer-on-arrival).
	vQuorumAt map[twoab.OperatorID]time.Duration

	// Per-op flags tracking which emissions have already been observed
	// by the DES (and thus already scheduled to peers). These flip from
	// false → true the FIRST time captureCascadeEmissions detects the
	// corresponding OwnXxx accessor returning a non-nil value. The
	// Instance internally enforces single-emission discipline per
	// emission kind per slot; these flags mirror that on the DES side
	// for arrival scheduling.
	valueMsgEmitted   map[twoab.OperatorID]bool
	noValueMsgEmitted map[twoab.OperatorID]bool
	commitEmitted     map[twoab.OperatorID]bool

	// crashed[op] = true for completely-offline operators. Wire behavior is
	// suppressed by crashOverlay + the emit guards; this set lets outcome()
	// exclude them from the decided set and stamp Err="offline".
	crashed map[twoab.OperatorID]bool

	// resolveDeadline is the slot offset past which late-arriving
	// emissions trigger an evtResolveRerun on the receiver (analog of
	// OBFT's "re-running on late KindCommit arrivals"). Set to the
	// scheduled evtResolve time at start; subsequent events use this to
	// gate the late re-resolve path.
	resolveDeadline time.Duration

	canonValues map[int]twoab.Value
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

	crashed := make(map[twoab.OperatorID]bool, len(cfg.Crashed))
	for _, op := range cfg.Crashed {
		crashed[twoab.OperatorID(op)] = true
	}

	return &sim{
		Engine:            desim.NewEngine(cfg.Seed, cfg.TraceEnabled),
		cfg:               cfg,
		operators:         operators,
		pubShares:         pubShares,
		clusterPub:        clusterPub,
		canonValues:       canonValues,
		instances:         make(map[twoab.OperatorID]*twoab.Instance, N),
		resolved:          make(map[twoab.OperatorID]*twoab.Output, N),
		resolvedAt:        make(map[twoab.OperatorID]time.Duration, N),
		resolveErrs:       make(map[twoab.OperatorID]error, N),
		crashed:           crashed,
		vQuorumAt:         make(map[twoab.OperatorID]time.Duration, N),
		valueMsgEmitted:   make(map[twoab.OperatorID]bool, N),
		noValueMsgEmitted: make(map[twoab.OperatorID]bool, N),
		commitEmitted:     make(map[twoab.OperatorID]bool, N),
	}, nil
}

// Mesh / Network / Bandwidth satisfy desim.Host (Now/Rng/Schedule/Run/Trace
// come from the embedded *desim.Engine).
func (s *sim) Mesh() *ct.MeshTopology         { return s.cfg.Mesh }
func (s *sim) Network() ct.NetworkModel       { return s.cfg.Network }
func (s *sim) Bandwidth() *ct.BandwidthReport { return s.cfg.Bandwidth }

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
		Height:       1,
		ClusterID:    [32]byte{0x01, 0x02, 0x03},
		Operators:    s.operators,
		F:            (s.cfg.N - 1) / 3,
		Layers:       layers,
		TPhase2a:     s.cfg.TPhase2a,
		SafetyBuffer: s.cfg.SafetyBuffer,
		BTT:          s.cfg.BTT,
	}
	if err := cfgTwoab.Validate(); err != nil {
		// Validate failures at this point mean the SimConfig translates
		// to an operating-point-incompatible twoab.Config (e.g. TPhase2a
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
		if s.signers == nil {
			s.signers = make(map[twoab.OperatorID]obft.Signer, s.cfg.N)
		}
		s.signers[op] = signer
	}

	// Slot-deadline for the schedule-anchored final Resolve sweep. In
	// 2abOBFT there is no RoundEndOffset() — Phase 2b is dynamic and the
	// only hard wall is the runner-level RelayCutoff. The adapter
	// schedules a final Resolve sweep AFTER the typical Phase-2b
	// settle window.
	//
	// The post-TPhase2a window is the MAX of the two
	// mutually-exclusive resolve paths, not their sum:
	//   - σ-ward: 1·BTT (KindValue prop) + SafetyBuffer (reflood tail).
	//   - NR-ward fall-through to L_1: 2·BTT (KindNoValue → KindCommit-NR
	//     → peer arrival → aggregate → decrypt L_1 entries).
	// A slot goes σ-ward XOR NR-ward, never both sequentially, so:
	//   window = max(1·BTT + SafetyBuffer, 2·BTT) = 1·BTT + max(SafetyBuffer, 1·BTT).
	// This must stay consistent with adapter.go's resolveBudget (which
	// derives TPhase2a from the same window); both use the max-form so
	// resolveDeadline reconstructs to exactly maxDeadline below. See
	// adapter.go Run() + docs/2abOBFT.md §Timing parameters (resolve window max-form).
	s.resolveDeadline = s.cfg.TPhase2a + s.cfg.BTT + max(s.cfg.SafetyBuffer, s.cfg.BTT) + s.cfg.Epsilon3
	// Clamp to before the runner-level deadline so the resolve still
	// has time to broadcast a cert and have it land before
	// RelayCutoff − HeaderSubmitHeadroom.
	maxDeadline := s.cfg.RelayCutoff - s.cfg.HeaderSubmitHeadroom - phase3JitterBuffer
	if s.resolveDeadline > maxDeadline {
		s.resolveDeadline = maxDeadline
	}
	if s.resolveDeadline <= 0 {
		s.resolveDeadline = s.cfg.RelayCutoff
	}

	for k := 0; k < K; k++ {
		s.Schedule(s.cfg.FetchAt[k], &evtLeaderFetch{layer: k})
	}
	// Phase-2a fire-instant: every op simultaneously calls
	// MaybeFirePhase2a per spec §Phase 2a. (Per-op offset is not used in
	// the protocol — fire-time is a single cluster-wide slot offset.)
	s.Schedule(cfgTwoab.TPhase2a, &evtPhase2aFire{})
	// Final schedule-anchored Resolve sweep. Captures any op that
	// hasn't yet decided through the dynamic Phase-2b cascade (e.g.,
	// late-arriving cluster state).
	s.Schedule(s.resolveDeadline, &evtResolve{})
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
			// so Terminated stays satisfied and the op is excluded from
			// SingleV / HonestAgreement (decided-only checks).
			out.perOp[ct.OperatorID(op)] = rawOpOutcome{decided: false, layer: -1, err: "offline"}
			continue
		}
		o := rawOpOutcome{decided: false, layer: -1}
		if res := s.resolved[op]; res != nil {
			o.decided = true
			o.layer = res.Layer
			o.value = append([]byte{}, res.Value...)
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
			if !o.decided {
				var rerr *twoab.ResolveError
				if errors.As(err, &rerr) && rerr.Reason == twoab.ResolveFailureDeadlock {
					if rerr.StoppedAtLayer > out.deadlockLayer {
						out.deadlockLayer = rerr.StoppedAtLayer
					}
				}
			}
		}
		o.evidenceByRule = evidenceByRule(s.instances[op].Evidence())
		out.perOp[ct.OperatorID(op)] = o
	}
	out.deadlockKind = classifyDeadlock(s, out.deadlockLayer)
	return out
}

// classifyDeadlock sub-classifies a deadlock (layer ≥ 0) by polling each
// live instance's σ-eligibility at the stuck layer: a host-invalid verdict
// anywhere means a validity-divergence wedge; otherwise a missing-bundle
// cohort with no equivocation means a recoverable propagation stall; the
// remainder (σ split across values, e.g. 1-1-1 equivocation) is a genuine
// no-single-value wedge. See classifyTwoabMiss.
func classifyDeadlock(s *sim, layer int) deadlockKind {
	if layer < 0 {
		return deadlockNone
	}
	var hostInvalid, noBundle, equiv bool
	for _, op := range s.operators {
		if s.crashed[op] {
			continue
		}
		switch s.instances[op].WhyNotSigma(layer) {
		case twoab.SigmaBlockHostInvalid:
			hostInvalid = true
		case twoab.SigmaBlockNoBundle:
			noBundle = true
		case twoab.SigmaBlockEquivocation:
			equiv = true
		}
	}
	switch {
	case hostInvalid:
		return deadlockValidity
	case noBundle && !equiv:
		return deadlockUndelivered
	default:
		return deadlockSplit
	}
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
// the per-sim MeshTopology with dedup + reflood.
func (s *sim) emitToAll(from twoab.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, build func(to twoab.OperatorID) desim.Event) {
	if s.cfg.Mesh != nil {
		s.emitMesh(from, kind, layer, bytes, extraDelay, s.operators, build)
		return
	}
	s.emitDirect(from, kind, layer, bytes, extraDelay, s.operators, build)
}

// emitDirect is the full-fanout transport path.
func (s *sim) emitDirect(from twoab.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, recipients []twoab.OperatorID, build func(to twoab.OperatorID) desim.Event) {
	// Crashed operators emit nothing on any path. Belt-and-suspenders with
	// the crashOverlay gates (which also stop the build/aggregator-record
	// step); this catches the Phase-2b commit path that has no Allow gate.
	if s.crashed[from] {
		return
	}
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

// emitMesh publishes via the per-sim MeshTopology. Behavior parallels the
// OBFT adapter's emitMesh.
func (s *sim) emitMesh(from twoab.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, recipients []twoab.OperatorID, build func(to twoab.OperatorID) desim.Event) {
	// Crashed operators are absent from the mesh topology — guard before
	// NodeForOperator (which would panic on a missing op) and emit nothing.
	if s.crashed[from] {
		return
	}
	mesh := s.cfg.Mesh
	fromOp := ct.OperatorID(from)
	fromNode := mesh.NodeForOperator(fromOp)
	id := mesh.NewMsgID()
	mesh.MarkSeen(fromNode, id)
	// Wrap the native-typed builder to the shared transport's ct.OperatorID
	// signature; the twoab↔ct conversion lands here, at the single
	// mesh-publish boundary.
	meshBuild := func(to ct.OperatorID) desim.Event { return build(twoab.OperatorID(to)) }
	desim.CacheArrivalForGossip(s, fromNode, fromNode, id, kind, layer, bytes, meshBuild)
	fromEP := mesh.EndpointFor(fromNode)
	for _, neighbor := range mesh.Neighbors(fromNode) {
		isProto := mesh.IsProtocol(neighbor)
		if isProto {
			toOp := twoab.OperatorID(mesh.OperatorForNode(neighbor))
			if !slices.Contains(recipients, toOp) {
				continue
			}
			if !s.cfg.Byz.AllowDelivery(from, toOp, kind) {
				continue
			}
		}
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

// markValueMsgEmitted marks `op` as having broadcast its ValueMsg
// (either Phase-2a fire-time or NoValue→Value upgrade). Subsequent
// captureCascadeEmissions calls skip the ValueMsg re-broadcast path
// for this op.
func (s *sim) markValueMsgEmitted(op twoab.OperatorID) {
	s.valueMsgEmitted[op] = true
}

// markNoValueMsgEmitted marks `op` as having broadcast its NoValueMsg.
func (s *sim) markNoValueMsgEmitted(op twoab.OperatorID) {
	s.noValueMsgEmitted[op] = true
}

// markCommitEmitted marks `op` as having broadcast its Commit (whether
// Phase-2a NRDirect or Phase-2b NR).
func (s *sim) markCommitEmitted(op twoab.OperatorID) {
	s.commitEmitted[op] = true
}
