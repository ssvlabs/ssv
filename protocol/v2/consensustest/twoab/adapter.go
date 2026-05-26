// Package twoab is the 2abOBFT adapter for consensustest. It wraps the
// real twoab.Instance under a virtual-time discrete-event simulator and
// translates abstract consensustest scenarios into 2ab-internal byz
// patterns.
//
// Structurally mirrors protocol/v2/consensustest/obft/ (the bare OBFT
// adapter) — same DES shape, same per-recipient cloning discipline,
// same evidence-rule-name convention. The 2ab-specific delta vs base is
// the Phase-2a coordination broadcast (every operator emits one of
// {KindValue, KindNoValue, KindCommit-NRDirect} at the Phase-2a
// fire-instant) and Phase 2b being dynamic — commits fire via the
// protocol's per-tick afterStateDelta cascade rather than at a fixed
// Phase-2b-start event.
package twoab

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Adapter-internal constants. Mirrors the OBFT adapter — these are
// operator-side reserves (BLS aggregation / IBE walk CPU cost, residual
// scheduling jitter).
const (
	epsilon3           = 50 * time.Millisecond
	phase3JitterBuffer = 50 * time.Millisecond
)

// Protocol is the 2abOBFT adapter. Use as `twoab.Protocol{}` for the
// canonical variant (SafetyBuffer = cfg.SafetyBuffer, matching bare OBFT's
// structural budget), or with `SafetyBufferOverride` to model a tighter
// or looser deployment.
type Protocol struct {
	// VariantName overrides the reported protocol name. Empty → "2abOBFT".
	VariantName string

	// SafetyBufferOverride, when non-nil, sets the SafetyBuffer used to
	// widen the σ-pool absorption budget post-TPhase2a. Default (nil)
	// uses cfg.SafetyBuffer so 2abOBFT matches bare OBFT's total post-
	// broadcast structural budget at default. Lower SafetyBuffer (e.g.
	// 300ms / 500ms) reclaims MEV-fetch headroom at the cost of σ-pool-
	// fill tolerance: the cluster commits to slot-miss rather than wait
	// for late peer KindValue arrivals when the network's per-hop
	// latency tail exceeds 1·BTT.
	//
	// Cascade-window semantics: KindValue carries the σ partial directly,
	// so the cluster's σ-pool[V_0] fills in 1 hop from TPhase2a.
	// SafetyBuffer plays the role of "σ-pool fill absorption budget for
	// IHAVE/IWANT recovery when initial KindValue eager-push doesn't reach
	// all honest peers" — conceptually close to OBFT's SafetyBuffer role (a
	// lazy-pull recovery window). The leader's pre-Phase-2a window
	// (`B_0 = 1·BTT`) stays at the structural minimum; ops who don't
	// observe V by TPhase2a fire KindNoValue and upgrade to KindValue once
	// the bundle (or peer KindValue carrying the forwarded witness)
	// arrives — that upgrade is the σ-side terminal emission, so it still
	// benefits from the SafetyBuffer absorption budget for late peer
	// observations.
	//
	// Crossover: the resolve window is max(1·BTT + SafetyBuffer, 2·BTT)
	// (a slot resolves σ-ward XOR NR-ward, so reserve the max not the
	// sum). SafetyBuffer therefore only widens the window ABOVE the
	// 1·BTT crossover — below it the 2·BTT NR-fall-through path dominates,
	// so a smaller SafetyBuffer reclaims MEV-fetch headroom for nothing
	// extra. See docs/2abOBFT.md §Timing parameters (SafetyBuffer crossover).
	SafetyBufferOverride *time.Duration

	// BaselineOnly marks a variant that only runs on Baseline-group
	// (Healthy) scenarios — RunBatch renders it n/a on adversarial
	// scenarios. Set on the cushion-sensitivity rungs (X-0 / X-300 / X-500).
	BaselineOnly bool
}

func (p Protocol) Name() string {
	if p.VariantName != "" {
		return p.VariantName
	}
	return "2abOBFT"
}

// IsBaselineOnly reports whether this variant runs only on Baseline-group
// scenarios (see BaselineOnly). Consumed by RunBatch.
func (p Protocol) IsBaselineOnly() bool { return p.BaselineOnly }

// safetyBuffer returns the SafetyBuffer for this variant: the override
// if set, otherwise cfg.SafetyBuffer (the spec default that matches bare
// OBFT's structural budget).
func (p Protocol) safetyBuffer(cfg ct.SimConfig) time.Duration {
	if p.SafetyBufferOverride != nil {
		return *p.SafetyBufferOverride
	}
	return cfg.SafetyBuffer
}

func (p Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		// SimConfig.Validate covers cluster topology (N/K/Operators) and
		// the slot constants. Wrap as ErrConfigOutOfEnvelope so the
		// framework renders these cells as red 0% rather than n/a.
		return ct.Outcome{}, fmt.Errorf("%w: %v", ct.ErrConfigOutOfEnvelope, err)
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}
	// Layer crash suppression on top of the translated byz pattern: crashed
	// operators emit nothing and receive nothing (overlay), are absent from
	// the mesh topology (MakeMeshTopology), and are reported offline by
	// sim.outcome(). Composes with any Kind; the sets are disjoint per Validate.
	if len(cfg.Byz.Crashed) > 0 {
		internal = crashOverlay{internalByz: internal, crashed: newByzSet(cfg.Byz.Crashed)}
	}

	// 2abOBFT timing model (spec §Setting):
	//   - TPhase2a is the Phase-2a fire-instant; every op emits one of
	//     {KindValue (with σ partial), KindNoValue, KindCommit-NRDirect}
	//     at this offset.
	//   - T0Broadcast = TPhase2a − BTT is the L_0 leader's broadcast
	//     time target — V_0 has 1·BTT to propagate before Phase 2a fires.
	//   - SafetyBuffer shifts TPhase2a earlier in the slot, widening the
	//     post-TPhase2a σ-pool fill window (1-hop peer KindValue
	//     propagation). Default sizing is one HeartbeatInterval (700ms)
	//     so 2abOBFT and bare OBFT have the same total post-broadcast
	//     structural budget (both fold the same SafetyBuffer into their
	//     respective schedules).
	//
	// We pick TPhase2a to fit within the slot's submit pipeline: the
	// runner-level deadline is RelayCutoff − HeaderSubmitHeadroom; the
	// adapter reserves phase3JitterBuffer + epsilon3 for Phase 3 + cert
	// dispatch, plus 2·BTT + SafetyBuffer for the post-Phase-2a settle
	// window.
	//
	// **Cascade-depth note (sum→max reservation)**: the
	// L_0 σ-side cascade is 1 hop (KindValue carries the σ partial
	// directly), but the L_0 NR-side cascade for fall-through-to-L_1 is
	// 2 hops (KindNoValue → KindCommit-NR → peer arrival → NR-aggregate
	// → decrypt L_1 entries). A slot resolves σ-ward XOR NR-ward — the
	// two paths are mutually exclusive outcomes, never sequential — so
	// the post-TPhase2a window only needs the MAX of the two, not their
	// sum:
	//
	//	window = max(1·BTT + SafetyBuffer,  2·BTT)
	//	             └── σ: KindValue prop ┘  └ NR: 2-hop ┘
	//	             +     reflood tail
	//	       = 1·BTT + max(SafetyBuffer, 1·BTT)
	//
	// The earlier draft reserved the SUM (`2·BTT + SafetyBuffer`); the
	// originally-proposed flat `1·BTT` is rejected (it clips NR
	// fall-through). The max-form reclaims min(1·BTT, SafetyBuffer) of
	// MEV-fetch headroom by shifting TPhase2a later, while still covering
	// both worst-case paths. resolveDeadline's wall-clock is unchanged
	// (still clamps to RelayCutoff − HeaderSubmit − phase3JitterBuffer).
	// See docs/2abOBFT.md §Timing parameters (resolve window max-form) for the full
	// safety/liveness case-walk. At SafetyBuffer=0 the max degenerates
	// to 2·BTT (identical to the old sum) — eager-push configs unaffected.
	btt := cfg.BTT
	// SafetyBuffer: default = cfg.SafetyBuffer (matches bare OBFT's
	// structural budget); the SafetyBufferOverride variant field lets
	// stresstest variants exercise tighter / looser configurations.
	safetyBuffer := p.safetyBuffer(cfg)

	resolveBudget := btt + max(safetyBuffer, btt) + epsilon3 + phase3JitterBuffer + cfg.HeaderSubmitHeadroom
	tPhase2a := cfg.RelayCutoff - resolveBudget
	if tPhase2a <= btt {
		// TPhase2a must be > BTT so T0Broadcast = TPhase2a − BTT is
		// positive (the Phase-1 broadcast time must land within the
		// slot). At extreme operating points (BTT too large for the
		// available slot budget, or SafetyBuffer set too aggressively)
		// the configuration is out of envelope.
		return ct.Outcome{}, fmt.Errorf(
			"%w: twoab adapter: derived TPhase2a=%v <= BTT=%v (RelayCutoff=%v SafetyBuffer=%v)",
			ct.ErrConfigOutOfEnvelope, tPhase2a, btt, cfg.RelayCutoff, safetyBuffer)
	}
	t0Broadcast := tPhase2a - btt

	// Per-layer broadcast budgets follow the spec's staggered shallow
	// schedule `B_k_shallow = (k+2)·BTT` (no SafetyBuffer term — the
	// SafetyBuffer instead widens the post-TPhase2a cascade via the
	// TPhase2a shift above, which transitively shifts every fetchAt[k]
	// earlier by the same amount).
	broadcastBudget, err := twoab.DefaultBroadcastBudget(cfg.K, btt, t0Broadcast)
	if err != nil {
		return ct.Outcome{}, fmt.Errorf("%w: twoab adapter: derive BroadcastBudget: %v",
			ct.ErrConfigOutOfEnvelope, err)
	}
	// Apply the spec's runtime clamp `T_broadcast_max_k = max(0,
	// T0Broadcast − B_k)`. A layer whose designed fetch lands before
	// slot start floors to 0.
	fetchAt := make([]time.Duration, cfg.K)
	for k := 0; k < cfg.K; k++ {
		fa := t0Broadcast - broadcastBudget[k]
		if fa < 0 {
			fa = 0
		}
		fetchAt[k] = fa
	}

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:                    cfg.N,
		K:                    cfg.K,
		Operators:            cfg.Operators,
		Crashed:              cfg.Byz.Crashed,
		TPhase2a:             tPhase2a,
		SafetyBuffer:         safetyBuffer,
		Epsilon3:             epsilon3,
		BTT:                  btt,
		HeaderSubmitHeadroom: cfg.HeaderSubmitHeadroom,
		FetchAt:              fetchAt,
		BroadcastBudget:      broadcastBudget,
		Network:              cfg.Network,
		Host:                 cfg.Host,
		Byz:                  internal,
		Seed:                 cfg.Seed,
		TraceEnabled:         cfg.TraceEnabled,
		BLSKeys:              cfg.BLSKeys,
		Aggregator:           ct.NewOfflineAggregator(cfg.N),
		Bandwidth:            &bw,
		Mesh:                 cfg.MakeMeshTopology(),
		RelayCutoff:          cfg.RelayCutoff,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	out := rawOut.toCT(desCfg.Aggregator, desCfg.Bandwidth)
	out.Byz = cfg.Byz
	out.CommitAttestation = computeAttestation(cfg, out)
	// Stamp the L_0 BFT_start-independence threshold = the unclamped
	// fetchAt[0] (= T0Broadcast − B_0, floored at 0). This is the `fa`
	// computed for k=0 above — the largest BFT_start for which L_0's schedule is
	// identical to BFT_start=0. The report UI reuses this (BFT_start=0)
	// cell at or below this value (see
	// Outcome.BFTStartIndependenceThreshold).
	//
	// The value is a pure function of cfg (BFT_start is not a sim
	// parameter — the sim always runs at BFT_start=0), so it's stamped
	// unconditionally on the single cell, regardless of decided/miss.
	bftIndep := t0Broadcast - broadcastBudget[0]
	if bftIndep < 0 {
		bftIndep = 0
	}
	out.BFTStartIndependenceThreshold = &bftIndep
	// Stamp the deciding-layer broadcast deadline (anchored at
	// t0Broadcast, per 2abOBFT's spec anchor). Shallow layers whose B_k
	// exceed t0Broadcast clamp to 0, matching the runtime rule
	// T_broadcast_max_k = max(0, T0Broadcast − B_k).
	if out.Decided && out.DecidedRound >= 0 && out.DecidedRound < len(broadcastBudget) {
		bt := t0Broadcast - broadcastBudget[out.DecidedRound]
		if bt < 0 {
			bt = 0
		}
		out.DecidingBroadcastTime = bt
	}
	// Pre-clip snapshot so MissReason can reference the layer the protocol
	// internally reached. See obft adapter's classifyOBFTMiss for the
	// shared rationale.
	preClipDecided := out.Decided
	preClipRound := out.DecidedRound
	preClipTime := out.DecisionTime
	deadline := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom
	// Match the OBFT/QBFT adapters: late-recovered decisions past
	// RelayCutoff − HeaderSubmitHeadroom can't be submitted in production
	// either, so the comparison clips them to MISS.
	ct.ClipLateDecision(&out, deadline)
	if !out.Decided {
		out.MissReason = classifyTwoabMiss(preClipDecided, preClipRound, preClipTime, deadline, rawOut.deadlockLayer, rawOut.deadlockKind)
	}
	return out, nil
}

// classifyTwoabMiss labels a non-decided 2abOBFT outcome. Regimes:
//
//   - Decided-but-clipped: a certificate formed at some layer but past the
//     submit deadline → "ready to submit at layer X, past the deadline".
//   - Deadlock walk (ResolveFailureDeadlock): no certificate formed and the
//     Phase-3 walk stalled at a layer (σ-pool < qV, NR-pool < qEnc). This
//     splits by deadlockKind (computed in des.go/classifyDeadlock from
//     host-rejection + distinct-retained-value evidence; stall by default):
//   - undelivered → a recoverable propagation stall: a single proposed
//     value that didn't reach a σ-quorum in time (typically undelivered to
//     the stuck cohort). NOT a protocol wedge — the instance self-heals the
//     instant the value arrives; the slot misses only because the value
//     never reached a σ-quorum before the relay deadline (the degraded-mesh
//     tail). Same species as QBFT's non-delivery miss.
//   - validity → a validity-divergence wedge: the stuck cohort holds the
//     value but its host verdict is not-valid, so σ-recovery is
//     impossible even with perfect delivery and the cannot-σ gate bars
//     the NR-default. A genuine 2ab-specific deadlock (QBFT escapes via
//     round-change to a fresh value).
//   - split → no single value reaches qV (e.g. 1-1-1 leader equivocation)
//     and σ-locked operators can't pivot to NR. Also a genuine wedge
//     QBFT escapes by re-proposing.
//     See docs/2abOBFT.md §Liveness.
//   - Exhaustion: walked all K layers (NR-quorums advanced) without a
//     σ-quorum → "never assembled a threshold signature at any layer".
func classifyTwoabMiss(preDecided bool, preRound int, preTime, deadline time.Duration, deadlockLayer int, kind deadlockKind) string {
	if preDecided && preTime > deadline {
		return fmt.Sprintf("Cluster ready to submit at layer %d, past the submit deadline", preRound)
	}
	if deadlockLayer >= 0 {
		switch kind {
		case deadlockValidity:
			return fmt.Sprintf("Cluster deadlocked at layer %d (validity split — σ impossible for the dissenting cohort, NR-default gated)", deadlockLayer)
		case deadlockSplit:
			return fmt.Sprintf("Cluster deadlocked at layer %d (σ split across values, none reaching qV; NR-default gated)", deadlockLayer)
		default: // deadlockUndelivered
			return fmt.Sprintf("Cluster stalled at layer %d — value didn't reach σ-quorum in time (undelivered)", deadlockLayer)
		}
	}
	return "Cluster never assembled a threshold signature at any layer"
}

// computeAttestation populates Outcome.CommitAttestation from data already
// visible at the adapter boundary. Each *Checked flag is set only when this
// adapter actually performs the corresponding cross-check; the framework
// treats unset flags as "uninstrumented, no violation reportable".
//
// Instrumented (mirrors OBFT base — see obft/adapter.go computeAttestation
// docstring for full rationale on each invariant):
//   - OBFTCommitKindValid (C2): naive — L_0 ⇒ "sigma"; L_k>0 ⇒ "nr".
//     Descriptive tag only; check at safety.go validates kind ∈
//     {"sigma", "nr"}.
//   - Equivocation: Rule 2 + Rule 3 + Rule 6a (2ab-specific) evidence
//     fires count into EquivocationsObserved. EquivocationsAccepted = 0
//     (see C4 deferral note below).
//
// Left uninstrumented (separate follow-ups):
//   - QuorumBackedDecision (C1): aggregator's SigmaCardinality
//     underapproximates the protocol's σ-pool view in scenarios that
//     combine plaintext leader-σ_V with chain-decrypted peer partials, or
//     under partition occlusion. False-positives flag legitimate
//     decisions. SigmaCardinality plumbing is added in this commit for
//     future use; the safety check needs protocol-side per-decision
//     quorum-count emission from twoab.Instance.Resolve.
//   - NoEquivocationAccepted (C4): real EquivocationsAccepted count needs
//     per-emitter visibility — bucket 2's SigmaByEmitter map.
//   - OBFTHostValidityRespect (C3): 2ab's host re-validation at Phase-2a /
//     Phase-2b means a layer-naive comparison over-reports. Requires
//     plumbing per-op acceptance-layer through the DES boundary.
func computeAttestation(_ ct.SimConfig, out ct.Outcome) ct.CommitAttestation {
	att := ct.CommitAttestation{
		EquivocationChecked: true,
	}

	if out.Decided {
		att.OBFTCommitKindChecked = true
		if out.DecidedRound == 0 {
			att.OBFTCommitKind = "sigma"
		} else {
			att.OBFTCommitKind = "nr"
		}
	}

	for _, oo := range out.PerOp {
		for rule, n := range oo.EvidenceByRule {
			if rule == RuleLeaderEquivocation ||
				rule == RuleCrossOnionEquivocation ||
				rule == RulePhase2Equivocation {
				att.EquivocationsObserved += n
			}
		}
	}

	return att
}

// desConfig is the 2ab-DES-internal configuration, built by Run.
type desConfig struct {
	N                    int
	K                    int
	Operators            []ct.OperatorID
	Crashed              []ct.OperatorID // completely-offline operators (subset of Operators)
	TPhase2a             time.Duration   // forwarded to twoab.Config.TPhase2a
	SafetyBuffer         time.Duration   // forwarded to twoab.Config.SafetyBuffer
	Epsilon3             time.Duration   // Phase-3 walk per-layer cost
	BTT                  time.Duration
	HeaderSubmitHeadroom time.Duration
	FetchAt              []time.Duration
	BroadcastBudget      []time.Duration
	Network              ct.NetworkModel
	Host                 ct.HostPattern
	Byz                  internalByz
	Seed                 int64
	TraceEnabled         bool
	BLSKeys              *ct.BLSKeys
	Aggregator           *ct.OfflineAggregator
	Bandwidth            *ct.BandwidthReport
	Mesh                 *ct.MeshTopology // nil when DeliveryDirect
	// RelayCutoff is the slot's hard submit deadline (carried over
	// from SimConfig). Used to bound the gossip-heartbeat sequence.
	RelayCutoff time.Duration
}

// deadlockKind sub-classifies a ResolveFailureDeadlock walk outcome by root
// cause, so the classifier can separate a recoverable propagation stall from
// the genuine 2ab-specific wedges. Computed in des.go/classifyDeadlock from
// host-rejection and distinct-retained-value evidence across the cluster
// (stall by default). See classifyTwoabMiss.
type deadlockKind int

const (
	deadlockNone        deadlockKind = iota // no deadlock (deadlockLayer < 0)
	deadlockUndelivered                     // propagation stall: single value never reached a σ-quorum in time (recoverable)
	deadlockValidity                        // validity-divergence wedge: some op host-rejected the value
	deadlockSplit                           // σ split across ≥ 2 values, none reaching qV (leader equivocation)
)

// rawOutcome is the 2ab-internal outcome before translation to ct.Outcome.
type rawOutcome struct {
	decided      bool
	decisionTime time.Duration
	layer        int
	value        []byte
	perOp        map[ct.OperatorID]rawOpOutcome
	trace        []ct.TraceEntry
	// deadlockLayer mirrors the OBFT adapter's: deepest layer at which
	// any non-decided op hit ResolveFailureDeadlock. -1 when none.
	deadlockLayer int
	// deadlockKind sub-classifies the deadlock (when deadlockLayer ≥ 0).
	deadlockKind deadlockKind
}

type rawOpOutcome struct {
	decided              bool
	layer                int
	value                []byte
	time                 time.Duration
	err                  string
	evidenceByRule       map[string]int
	resolveLayerAttempts []twoab.LayerAttempt
}

func (r rawOutcome) toCT(agg *ct.OfflineAggregator, bw *ct.BandwidthReport) ct.Outcome {
	out := ct.Outcome{
		Decided:      r.decided,
		DecisionTime: r.decisionTime,
		DecidedValue: append([]byte(nil), r.value...),
		DecidedRound: r.layer,
		PerOp:        make(map[ct.OperatorID]ct.OperatorOutcome, len(r.perOp)),
		Trace:        r.trace,
	}
	for op, oo := range r.perOp {
		var evMap map[string]int
		if len(oo.evidenceByRule) > 0 {
			evMap = make(map[string]int, len(oo.evidenceByRule))
			for k, v := range oo.evidenceByRule {
				evMap[k] = v
			}
		}
		bandwidthOut := int64(0)
		bandwidthIn := int64(0)
		if bw != nil {
			bandwidthOut = bw.PerOperatorOut[op]
			bandwidthIn = bw.PerOperatorIn[op]
		}
		var layerAttempts []ct.LayerAttempt
		if len(oo.resolveLayerAttempts) > 0 {
			layerAttempts = make([]ct.LayerAttempt, len(oo.resolveLayerAttempts))
			for i, la := range oo.resolveLayerAttempts {
				layerAttempts[i] = ct.LayerAttempt{
					Layer:         la.Layer,
					SigmaPoolSize: la.SigmaPoolSize,
					QV:            la.QV,
					SigmaReached:  la.SigmaReached,
					Decided:       la.Decided,
					NRPoolSize:    la.NRPoolSize,
					QEnc:          la.QEnc,
					NRReached:     la.NRReached,
				}
			}
		}
		// B5 — adapter-side single-decision-per-op guard. See OBFT
		// adapter's same guard for the rationale (defensive
		// future-proofing; protocol-side B5 enforced via i.committed /
		// i.ended in twoab.Instance).
		if _, exists := out.PerOp[op]; exists {
			panic(fmt.Sprintf("consensustest/twoab: adapter wrote PerOp[%d] twice — B5 violation (single-decision-per-op)", op))
		}
		out.PerOp[op] = ct.OperatorOutcome{
			Decided:              oo.decided,
			Value:                append([]byte(nil), oo.value...),
			Round:                oo.layer,
			Time:                 oo.time,
			Err:                  oo.err,
			EvidenceByRule:       evMap,
			BandwidthOut:         bandwidthOut,
			BandwidthIn:          bandwidthIn,
			ResolveLayerAttempts: layerAttempts,
		}
	}
	if agg != nil {
		out.OfflineAgg = agg.AttemptAll()
	}
	if bw != nil {
		out.Bandwidth = *bw
	}
	return out
}

// valuePrefix returns a hex-prefix of v for diagnostic dumps.
func valuePrefix(v []byte) string {
	if len(v) == 0 {
		return "<empty>"
	}
	if len(v) > 6 {
		return fmt.Sprintf("%x", v[:6])
	}
	return fmt.Sprintf("%x", v)
}

// evidenceByRule maps a slice of twoab.Evidence to per-rule fire counts.
func evidenceByRule(evs []twoab.Evidence) map[string]int {
	if len(evs) == 0 {
		return nil
	}
	m := make(map[string]int)
	for _, e := range evs {
		m[ruleKey(e)]++
	}
	return m
}

// Rule-name constants for OperatorOutcome.EvidenceByRule keys. Follow the
// "<Protocol>/Rule<N>/<Description>" convention shared with the OBFT
// adapter, distinguished by the "2abOBFT/" prefix.
const (
	RuleCrossSigning           = "2abOBFT/Rule1/CrossSigning"
	RuleLeaderEquivocation     = "2abOBFT/Rule2/LeaderEquivocation"
	RuleCrossOnionEquivocation = "2abOBFT/Rule3/CrossCommitEquivocation"
	RuleFakeEncryptedPresence  = "2abOBFT/Rule4/FakeEncryptedPresence"
	RuleFakePlaintextSigma     = "2abOBFT/Rule5/FakePlaintextSigma"
	RulePhase2Equivocation     = "2abOBFT/Rule6a/Phase2Equivocation"
	RuleUnknown                = "2abOBFT/Unknown"
)

func ruleKey(e twoab.Evidence) string {
	switch e.Rule {
	case twoab.EvidenceCrossSigning:
		return RuleCrossSigning
	case twoab.EvidenceLeaderEquivocation:
		return RuleLeaderEquivocation
	case twoab.EvidenceCrossCommitEquivocation:
		return RuleCrossOnionEquivocation
	case twoab.EvidenceFakeEncryptedPresence:
		return RuleFakeEncryptedPresence
	case twoab.EvidenceFakePlaintextSigma:
		return RuleFakePlaintextSigma
	case twoab.EvidencePhase2Equivocation:
		return RulePhase2Equivocation
	default:
		return RuleUnknown
	}
}
