// Package obft is the OBFT adapter for consensustest. It wraps the real
// obft.Instance under a virtual-time discrete-event simulator and
// translates abstract consensustest scenarios into OBFT-internal byz
// patterns.
package obft

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
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

	// OBFT timing per OBFT.md §Application: T_commit = RelayCutoff − 3 BTT,
	// Δ_2 = 2 BTT, Δ_3 ≈ 0.5 BTT. (D, δ) split is 0.75/0.25; OBFT.Config only
	// requires D+δ = BTT and both > 0.
	d := cfg.BTT * 3 / 4
	delta := cfg.BTT - d
	if delta == 0 {
		delta = time.Nanosecond
		d = cfg.BTT - delta
	}
	tCommit := cfg.RelayCutoff - 3*cfg.BTT
	delta2 := 2 * cfg.BTT
	delta3 := cfg.BTT / 2
	if delta3 < cfg.BTT-d-delta+time.Millisecond {
		delta3 = cfg.BTT - d - delta + time.Millisecond
	}
	if delta3 <= 0 {
		delta3 = 100 * time.Millisecond
	}

	// Anchor V_0 at T_broadcast_max = T_commit − 2·BTT (the cap obft.Config
	// validates) and step deeper layers back by 1 BTT each to preserve the
	// asymmetric schedule. The spec's more-aggressive V_0 = T_commit − 1·BTT
	// schedule is not validated by obft.Config; CONSENSUS-TEST-PLAN.md
	// "Open questions" tracks the trade-off.
	K := 4
	tBroadcastMax := tCommit - 2*cfg.BTT
	fetchAt := make([]time.Duration, K)
	for k := 0; k < K; k++ {
		fetchAt[k] = tBroadcastMax - time.Duration(k)*cfg.BTT
		if fetchAt[k] < 0 {
			fetchAt[k] = 0
		}
	}

	desCfg := desConfig{
		N:           cfg.N,
		K:           K,
		TCommit:     tCommit,
		Delta2:      delta2,
		Delta3:      delta3,
		D:           d,
		Delta:       delta,
		FetchAt:     fetchAt,
		Network:     cfg.Network,
		Host:        cfg.Host,
		Byz:         internal,
		Seed:        cfg.Seed,
		TraceEnabled: cfg.TraceEnabled,
		BLSKeys:     cfg.BLSKeys,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	return rawOut.toCT(), nil
}

// desConfig is the OBFT-DES-internal configuration, built by Run.
type desConfig struct {
	N            int
	K            int
	TCommit      time.Duration
	Delta2       time.Duration
	Delta3       time.Duration
	D            time.Duration
	Delta        time.Duration
	FetchAt      []time.Duration
	Network      ct.NetworkModel
	Host         ct.HostPattern
	Byz          internalByz
	Seed         int64
	TraceEnabled bool
	BLSKeys      *ct.BLSKeys
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
	decided       bool
	layer         int
	value         []byte
	time          time.Duration
	err           string
	evidenceCount int
}

func (r rawOutcome) toCT() ct.Outcome {
	out := ct.Outcome{
		Decided:      r.decided,
		DecisionTime: r.decisionTime,
		DecidedValue: append([]byte(nil), r.value...),
		DecidedRound: r.layer,
		PerOp:        make(map[ct.OperatorID]ct.OperatorOutcome, len(r.perOp)),
		Trace:        r.trace,
	}
	if !r.decided {
		out.DecidedRound = -1
	}
	for op, oo := range r.perOp {
		out.PerOp[op] = ct.OperatorOutcome{
			Decided:       oo.decided,
			Value:         append([]byte(nil), oo.value...),
			Round:         oo.layer,
			Time:          oo.time,
			Err:           oo.err,
			EvidenceCount: oo.evidenceCount,
		}
	}
	return out
}

// hashValue returns a hex-prefix of v for diagnostic dumps.
func hashValue(v []byte) string {
	if len(v) == 0 {
		return "<empty>"
	}
	if len(v) > 6 {
		return fmt.Sprintf("%x", v[:6])
	}
	return fmt.Sprintf("%x", v)
}
