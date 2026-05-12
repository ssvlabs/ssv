// Package obft is the OBFT adapter for consensustest. It wraps the real
// obft.Instance under a virtual-time discrete-event simulator and
// translates abstract consensustest scenarios into OBFT-internal byz
// patterns.
package obft

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftbase "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Protocol is the OBFT adapter. Use as `obft.Protocol{}` in tests.
type Protocol struct{}

func (Protocol) Name() string { return "OBFT" }

func (Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		// SimConfig.Validate covers schedule shape (BroadcastBudget,
		// FetchAt), T_commit positivity, and other operating-point-
		// derived constraints. Wrap as ErrConfigOutOfEnvelope so the
		// framework renders these cells as red 0% rather than n/a.
		return ct.Outcome{}, fmt.Errorf("%w: %v", ct.ErrConfigOutOfEnvelope, err)
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	tCommit := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom - cfg.Phase3JitterBuffer - cfg.Epsilon3 - cfg.Delta2

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:                     cfg.N,
		K:                     cfg.K,
		Operators:             cfg.Operators,
		TCommit:               tCommit,
		Delta2:                cfg.Delta2,
		Epsilon3:              cfg.Epsilon3,
		BTT:                   cfg.BTT,
		FetchAt:               cfg.FetchAt,
		BroadcastBudget:       cfg.BroadcastBudget,
		Network:               cfg.Network,
		Host:                  cfg.Host,
		Byz:                   internal,
		Seed:                  cfg.Seed,
		TraceEnabled:          cfg.TraceEnabled,
		BLSKeys:    cfg.BLSKeys,
		Aggregator: ct.NewOfflineAggregator(cfg.N),
		Bandwidth:  &bw,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	out := rawOut.toCT(desCfg.Aggregator, desCfg.Bandwidth)
	out.CommitAttestation = computeAttestation(cfg, out)
	// Clip late decisions to MISS so the outcome reflects deployed-protocol
	// behavior. Phase 3's evtResolveRerun can recover from a late-arriving
	// KindCommit past RoundEndOffset, but if the rebuilt decision lands past
	// RelayCutoff − HeaderSubmitHeadroom, production would miss the slot too
	// (no time left to broadcast the cert and submit). Mirrors the QBFT
	// adapter's deadline clip so the comparison is apples-to-apples.
	ct.ClipLateDecision(&out, cfg.RelayCutoff-cfg.HeaderSubmitHeadroom)
	return out, nil
}

// computeAttestation populates Outcome.CommitAttestation from data already
// visible at the adapter boundary. Each *Checked flag is set only when this
// adapter actually performs the corresponding cross-check; the framework
// treats unset flags as "uninstrumented, no violation reportable".
//
// Currently instrumented:
//   - Equivocation: Rule2 (LeaderEquivocation) + Rule3 (CrossOnion /
//     CommitEquivocation) evidence fires are counted into
//     EquivocationsObserved. EquivocationsAccepted stays at 0 — OBFT's
//     internal Rule3 enforcement excludes equivocating partials from σ /
//     NR quorums by construction, so any actually-accepted equivocation
//     would already manifest as a NoOfflineDoubleV / SingleV violation
//     upstream. The framework therefore needs no additional gate here; the
//     EquivocationsObserved count is diagnostic, distinguishing
//     "vacuously safe" runs (==0) from "tested safe" runs (>0).
//
// Left uninstrumented (need deeper introspection than the adapter boundary
// currently exposes — deferred to a follow-up):
//   - Quorum: would require plumbing the partial-signature count out of
//     obft.Instance.BuildCertificate. obft.Instance internally enforces
//     ≥ 2f+1 distinct valid partials before emitting Output; that
//     invariant is correctness-of-protocol, not currently re-verified at
//     the framework level.
//   - OBFTCommitKind: distinguishing σ-quorum-commit from NR-quorum-commit
//     requires inspecting which path obft.Instance took to build the cert
//     (direct L_0 σ-quorum vs. NR-unlocked deeper σ-reconstruction). The
//     final cert is always a σ-signature on V regardless of path.
//   - OBFTHostValidityRespect: OBFT's validate-once-and-lock property means
//     a layer-naive comparison (decided_layer vs current host verdict)
//     over-reports — scenarios like HostFlipMidSlot have ops legitimately
//     decide at L_1 on a V they accepted at L_0 when the host's L_1
//     verdict is "invalid". A correct check requires plumbing each op's
//     recorded acceptance-layer through the DES boundary.
func computeAttestation(_ ct.SimConfig, out ct.Outcome) ct.CommitAttestation {
	att := ct.CommitAttestation{
		EquivocationChecked: true,
	}

	for _, oo := range out.PerOp {
		for rule, n := range oo.EvidenceByRule {
			if rule == RuleLeaderEquivocation ||
				rule == RuleCrossOnionEquivocation ||
				rule == RuleCommitEquivocation {
				att.EquivocationsObserved += n
			}
		}
	}

	return att
}

// desConfig is the OBFT-DES-internal configuration, built by Run.
type desConfig struct {
	N               int
	K               int
	Operators       []ct.OperatorID
	TCommit         time.Duration
	Delta2          time.Duration
	Epsilon3        time.Duration // forwarded to obftbase.Config.Delta3 (= ε_3 per spec)
	BTT             time.Duration
	FetchAt         []time.Duration
	BroadcastBudget []time.Duration
	Network         ct.NetworkModel
	Host            ct.HostPattern
	Byz             internalByz
	Seed            int64
	TraceEnabled    bool
	BLSKeys         *ct.BLSKeys
	Aggregator      *ct.OfflineAggregator
	Bandwidth       *ct.BandwidthReport
}

// rawOutcome is the OBFT-internal outcome before translation to ct.Outcome.
type rawOutcome struct {
	decided      bool
	decisionTime time.Duration
	layer        int
	value        []byte
	perOp        map[ct.OperatorID]rawOpOutcome
	trace        []ct.TraceEntry
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
	// rawOutcome.layer is initialized to -1 in outcome() when no operator
	// decided, so DecidedRound is correct without a separate post-fixup.
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

// valuePrefix returns a hex-prefix of v for diagnostic dumps. Distinct from
// the consensustest package's hashValue (which returns [32]byte for use as
// map keys); these serve different purposes and shouldn't share a name.
func valuePrefix(v []byte) string {
	if len(v) == 0 {
		return "<empty>"
	}
	if len(v) > 6 {
		return fmt.Sprintf("%x", v[:6])
	}
	return fmt.Sprintf("%x", v)
}

// evidenceByRule maps a slice of obft.Evidence to per-rule fire counts using
// the framework's standard "OBFT/RuleN/Description" key convention.
func evidenceByRule(evs []obftbase.Evidence) map[string]int {
	if len(evs) == 0 {
		return nil
	}
	m := make(map[string]int)
	for _, e := range evs {
		m[ruleKey(e)]++
	}
	return m
}

// Rule-name constants for OperatorOutcome.EvidenceByRule keys. Shared
// between the per-emission classifier (ruleKey) and the consumer
// instrumentation (computeAttestation) so a rename happens in one place.
const (
	RuleCrossSigning           = "OBFT/Rule1/CrossSigning"
	RuleLeaderEquivocation     = "OBFT/Rule2/LeaderEquivocation"
	RuleCommitEquivocation     = "OBFT/Rule3/CommitEquivocation"
	RuleCrossOnionEquivocation = "OBFT/Rule3/CrossOnionEquivocation"
	RuleFakeEncryptedPresence  = "OBFT/Rule4/FakeEncryptedPresence"
	RuleFakePlaintextSigma     = "OBFT/Rule5/FakePlaintextSigma"
	RuleUnknown                = "OBFT/Unknown"
)

func ruleKey(e obftbase.Evidence) string {
	switch e.Rule {
	case obftbase.EvidenceCrossSigning:
		return RuleCrossSigning
	case obftbase.EvidenceLeaderEquivocation:
		return RuleLeaderEquivocation
	case obftbase.EvidenceCrossOnionEquivocation:
		// Layer == -1 indicates the top-level CommitEquivocation variant
		// (full Commit bodies); per-layer Layer ≥ 0 indicates the per-V σ
		// variant. Slashing layer treats them as the same fault but per-rule
		// telemetry distinguishes them.
		if e.Layer < 0 {
			return RuleCommitEquivocation
		}
		return RuleCrossOnionEquivocation
	case obftbase.EvidenceFakeEncryptedPresence:
		return RuleFakeEncryptedPresence
	case obftbase.EvidenceFakePlaintextSigma:
		return RuleFakePlaintextSigma
	default:
		return RuleUnknown
	}
}
