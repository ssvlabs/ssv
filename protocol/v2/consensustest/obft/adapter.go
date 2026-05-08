// Package obft is the OBFT adapter for consensustest. It wraps the real
// obft.Instance under a virtual-time discrete-event simulator and
// translates abstract consensustest scenarios into OBFT-internal byz
// patterns.
package obft

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Protocol is the OBFT adapter. Use as `obft.Protocol{}` in tests.
type Protocol struct{}

func (Protocol) Name() string { return "OBFT" }

func (Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		return ct.Outcome{}, err
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	tCommit := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom - cfg.Delta3 - cfg.Delta2

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:               cfg.N,
		K:               cfg.K,
		Operators:       cfg.Operators,
		TCommit:         tCommit,
		Delta2:          cfg.Delta2,
		Delta3:          cfg.Delta3,
		BTT:             cfg.BTT,
		FetchAt:         cfg.FetchAt,
		BroadcastBudget: cfg.BroadcastBudget,
		Network:         cfg.Network,
		Host:            cfg.Host,
		Byz:             internal,
		Seed:            cfg.Seed,
		TraceEnabled:    cfg.TraceEnabled,
		BLSKeys:         cfg.BLSKeys,
		Aggregator:      ct.NewOfflineAggregator(cfg.N),
		Bandwidth:       &bw,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	return rawOut.toCT(desCfg.Aggregator, desCfg.Bandwidth), nil
}

// desConfig is the OBFT-DES-internal configuration, built by Run.
type desConfig struct {
	N               int
	K               int
	Operators       []ct.OperatorID
	TCommit         time.Duration
	Delta2          time.Duration
	Delta3          time.Duration
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
func evidenceByRule(evs []obft.Evidence) map[string]int {
	if len(evs) == 0 {
		return nil
	}
	m := make(map[string]int)
	for _, e := range evs {
		m[ruleKey(e)]++
	}
	return m
}

func ruleKey(e obft.Evidence) string {
	switch e.Rule {
	case obft.EvidenceCrossSigning:
		return "OBFT/Rule1/CrossSigning"
	case obft.EvidenceLeaderEquivocation:
		return "OBFT/Rule2/LeaderEquivocation"
	case obft.EvidenceCrossOnionEquivocation:
		// Layer == -1 indicates the top-level CommitEquivocation variant
		// (full Commit bodies); per-layer Layer ≥ 0 indicates the per-V σ
		// variant. Slashing layer treats them as the same fault but per-rule
		// telemetry distinguishes them.
		if e.Layer < 0 {
			return "OBFT/Rule3/CommitEquivocation"
		}
		return "OBFT/Rule3/CrossOnionEquivocation"
	case obft.EvidenceFakeEncryptedPresence:
		return "OBFT/Rule4/FakeEncryptedPresence"
	case obft.EvidenceFakePlaintextSigma:
		return "OBFT/Rule5/FakePlaintextSigma"
	default:
		return "OBFT/Unknown"
	}
}
