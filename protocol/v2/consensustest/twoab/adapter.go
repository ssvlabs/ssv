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
// canonical variant (SafetyBuffer = cfg.RefloodDelay, matching bare OBFT's
// structural budget), or with `SafetyBufferOverride` to model a tighter
// or looser deployment.
type Protocol struct {
	// VariantName overrides the reported protocol name. Empty → "2abOBFT".
	VariantName string

	// SafetyBufferOverride, when non-nil, sets the SafetyBuffer used in
	// the per-layer broadcast-budget formula `B_k_shallow = (k+2)·BTT +
	// SafetyBuffer`. Default (nil) uses cfg.RefloodDelay (which matches
	// bare OBFT's structural budget so the two protocols have the same
	// MEV-fetch headroom at default).
	//
	// Lower SafetyBuffer (e.g. 300ms / 500ms) reclaims MEV-fetch headroom
	// at the cost of mesh-tail tolerance: the cluster commits to slot-
	// miss rather than wait for IHAVE/IWANT recovery when initial
	// propagation slips.
	SafetyBufferOverride *time.Duration
}

func (p Protocol) Name() string {
	if p.VariantName != "" {
		return p.VariantName
	}
	return "2abOBFT"
}

// safetyBuffer returns the SafetyBuffer for this variant: the override
// if set, otherwise cfg.RefloodDelay (the spec default that matches bare
// OBFT's structural budget).
func (p Protocol) safetyBuffer(cfg ct.SimConfig) time.Duration {
	if p.SafetyBufferOverride != nil {
		return *p.SafetyBufferOverride
	}
	return cfg.RefloodDelay
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

	// 2abOBFT timing model (spec §Setting):
	//   - TPhase2a is the Phase-2a fire-instant; every op emits one of
	//     {KindValue, KindNoValue, KindCommit-NRDirect} at this offset.
	//   - T0Broadcast = TPhase2a − BTT is the L_0 leader's broadcast
	//     time target — V_0 has 1·BTT to propagate before Phase 2a fires.
	//   - SafetyBuffer parameterizes the per-layer shallow B_k:
	//     B_k_shallow = (k+2)·BTT + SafetyBuffer. Default sizing uses
	//     SafetyBuffer = RefloodDelay so 2abOBFT and bare OBFT have the
	//     same total structural budget.
	//
	// We pick TPhase2a to fit within the slot's submit pipeline: the
	// runner-level deadline is RelayCutoff − HeaderSubmitHeadroom; the
	// adapter reserves phase3JitterBuffer + epsilon3 for Phase 3 + cert
	// dispatch. Phase 2b is dynamic (no scheduled deadline), so we don't
	// need a Δ_2b reserve — commits fire opportunistically through the
	// cascade. The slot deadline pool's lower bound is just the
	// fire-time + a few BTTs for the cascade to converge through
	// state-delta-driven Commit emissions; that's also where the
	// schedule-anchored final Resolve sweep lands.
	btt := cfg.BTT
	// Phase-2a fire-instant: enough room after fire for at least one
	// Phase-2a propagation cycle plus a Phase-2b cascade settle window
	// (Commit emissions cascade as op-state-delta-triggered; bound by
	// ~2·BTT for the typical Value → Commit-Signed sequence).
	resolveBudget := btt*2 + epsilon3 + phase3JitterBuffer + cfg.HeaderSubmitHeadroom
	tPhase2a := cfg.RelayCutoff - resolveBudget
	if tPhase2a <= btt {
		// TPhase2a must be > BTT so T0Broadcast = TPhase2a − BTT is
		// positive (the Phase-1 broadcast time must land within the
		// slot). At extreme operating points (BTT too large for the
		// available slot budget) the configuration is out of envelope.
		return ct.Outcome{}, fmt.Errorf(
			"%w: twoab adapter: derived TPhase2a=%v non-positive or <= BTT=%v (RelayCutoff=%v)",
			ct.ErrConfigOutOfEnvelope, tPhase2a, btt, cfg.RelayCutoff)
	}
	t0Broadcast := tPhase2a - btt

	// SafetyBuffer: default = cfg.RefloodDelay (matches bare OBFT's
	// structural budget); the SafetyBufferOverride variant field lets
	// stresstest variants exercise tighter / looser configurations.
	safetyBuffer := p.safetyBuffer(cfg)

	broadcastBudget, err := twoab.DefaultBroadcastBudget(cfg.K, btt, safetyBuffer, t0Broadcast)
	if err != nil {
		return ct.Outcome{}, fmt.Errorf("%w: twoab adapter: derive BroadcastBudget: %v",
			ct.ErrConfigOutOfEnvelope, err)
	}
	// Apply the spec's runtime clamp `T_broadcast_max_k = max(BFTStart,
	// T0Broadcast − B_k)`. BFTStart=0 preserves the legacy `if < 0`
	// clamp bit-exactly.
	bftStart := cfg.BFTStart
	fetchAt := make([]time.Duration, cfg.K)
	for k := 0; k < cfg.K; k++ {
		fa := t0Broadcast - broadcastBudget[k]
		if fa < bftStart {
			fa = bftStart
		}
		fetchAt[k] = fa
	}

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:                    cfg.N,
		K:                    cfg.K,
		Operators:            cfg.Operators,
		BFTStart:             bftStart,
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
	out.CommitAttestation = computeAttestation(cfg, out)
	// Stamp the deciding-layer broadcast deadline (anchored at
	// t0Broadcast, per 2abOBFT's spec anchor). Shallow layers whose B_k
	// exceed t0Broadcast clamp to BFTStart, matching the runtime rule
	// T_broadcast_max_k = max(BFTStart, T0Broadcast − B_k).
	if out.Decided && out.DecidedRound >= 0 && out.DecidedRound < len(broadcastBudget) {
		bt := t0Broadcast - broadcastBudget[out.DecidedRound]
		if bt < bftStart {
			bt = bftStart
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
		out.MissReason = classifyTwoabMiss(preClipDecided, preClipRound, preClipTime, deadline, rawOut.deadlockLayer)
	}
	return out, nil
}

// classifyTwoabMiss mirrors the OBFT classifier; structurally identical
// (decided-but-clipped vs deadlocked-mid-walk vs exhausted-K-layers),
// since the 2ab Phase-3 walk has the same two failure modes. See
// classifyOBFTMiss in the obft adapter for the rationale on each
// regime. 2abOBFT's Phase-2a coordination broadcast makes the HV1-style
// L_0 deadlock that bites OBFT recover via NR-quorum here, so in
// practice ResolveFailureDeadlock should be rare for 2abOBFT — when
// it appears it implies a different pathology than the OBFT case
// (e.g. σ AND NR pools BOTH degraded at the same layer).
func classifyTwoabMiss(preDecided bool, preRound int, preTime, deadline time.Duration, deadlockLayer int) string {
	if preDecided && preTime > deadline {
		return fmt.Sprintf("Cluster ready to submit at layer %d, past the submit deadline", preRound)
	}
	if deadlockLayer >= 0 {
		// Under v4: both pools short at this layer + cannot-σ gate blocks
		// σ-eligible ops from defaulting to NR + no T_commit hard wall →
		// cluster waits indefinitely until slot deadline. This shape is
		// distinct from OBFT's classic "σ-pool short, NR-pool failed to
		// reach qEnc" deadlock — in v4 it's "neither pool reaches its
		// threshold, gate prevents NR-default" — though both manifest
		// as the same per-layer stuck state.
		return fmt.Sprintf("Cluster deadlocked at layer %d (neither σ-quorum nor NR-quorum reaches; cannot-σ gate prevents NR-default)", deadlockLayer)
	}
	return "Cluster never assembled a threshold signature at any layer"
}

// computeAttestation populates Outcome.CommitAttestation from data already
// visible at the adapter boundary. Equivocation is counted from Rule 2
// (LeaderEquivocation) + Rule 3 (CrossCommitEquivocation, per-layer) +
// Rule 6a (Phase2Equivocation, 2ab-specific) evidence fires.
//
// Left uninstrumented (same as base; would require deeper Instance
// introspection):
//   - Quorum: would require plumbing the partial-signature count out of
//     twoab.Instance.BuildCertificate. The Instance internally enforces
//     ≥ 2f+1 distinct valid partials before emitting Output.
//   - 2abOBFTCommitKind (σ-quorum at L_0 vs NR-unlocked deeper σ): the
//     final cert is always a σ-signature on V regardless of path.
//   - OBFTHostValidityRespect: 2ab's host re-validation at Phase-2a /
//     Phase-2b sign time means the layer-naive comparison over-reports.
//     Requires plumbing per-op acceptance-layer through the DES boundary.
func computeAttestation(_ ct.SimConfig, out ct.Outcome) ct.CommitAttestation {
	att := ct.CommitAttestation{
		EquivocationChecked: true,
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
	BFTStart             time.Duration // forwarded to twoab.Config.BFTStart
	TPhase2a             time.Duration // forwarded to twoab.Config.TPhase2a
	SafetyBuffer         time.Duration // forwarded to twoab.Config.SafetyBuffer
	Epsilon3             time.Duration // Phase-3 walk per-layer cost
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
}

type rawOpOutcome struct {
	decided        bool
	layer          int
	value          []byte
	time           time.Duration
	err            string
	evidenceByRule map[string]int
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
		out.PerOp[op] = ct.OperatorOutcome{
			Decided:        oo.decided,
			Value:          append([]byte(nil), oo.value...),
			Round:          oo.layer,
			Time:           oo.time,
			Err:            oo.err,
			EvidenceByRule: evMap,
			BandwidthOut:   bandwidthOut,
			BandwidthIn:    bandwidthIn,
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
