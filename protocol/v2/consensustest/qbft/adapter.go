// Package qbft is the QBFT adapter for consensustest. It is a behavioral model
// of the PROPOSE / PREPARE / COMMIT / ROUND_CHANGE flow with round-change on
// timeout — not a wrapper of qbft.Instance, since the wire-format / spec
// fidelity layer isn't needed for cross-protocol scenario comparison
// (bit-exact QBFT coverage lives in protocol/v2/qbft/spectest).
package qbft

import (
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Protocol is the QBFT adapter. Use as `qbft.Protocol{}` in tests.
type Protocol struct {
	// RoundTimeout defaults to 2s (SSV production value).
	RoundTimeout time.Duration
	// MaxRounds caps round-change attempts before giving up. Default 4.
	MaxRounds int
}

func (Protocol) Name() string { return "QBFT" }

func (p Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		return ct.Outcome{}, err
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	rt := p.RoundTimeout
	if rt == 0 {
		rt = 2 * time.Second
	}
	maxRounds := p.MaxRounds
	if maxRounds == 0 {
		maxRounds = 4
	}

	// Anchor BFT_start so R2 success fits the relay deadline. R2 path is
	// RT (R1 timer) + 1 BTT (ROUND_CHANGE prop) + 3 BTT (PROPOSE/PREPARE/COMMIT)
	// + 1 BTT (post-consensus) = RT + 5 BTT past BFT_start. At Config A this
	// resolves to 0.9s, matching OBFT.md §Application's recommended BFT_start.
	bftStart := cfg.RelayCutoff - rt - 5*cfg.BTT - cfg.HeaderSubmitHeadroom
	if bftStart < 0 {
		bftStart = 0
	}

	desCfg := desConfig{
		N:            cfg.N,
		BTT:          cfg.BTT,
		RT:           rt,
		MaxRounds:    maxRounds,
		BFTStart:     bftStart,
		Network:      cfg.Network,
		Host:         cfg.Host,
		Byz:          internal,
		Seed:         cfg.Seed,
		TraceEnabled: cfg.TraceEnabled,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	out := rawOut.toCT()

	// The behavioral model runs round-changes up to MaxRounds × RT regardless
	// of wall time; the application's relay cutoff is what makes long chains
	// a slot miss in practice. Clip post-deadline decisions to MISS so the
	// outcome reflects the deployed-protocol behavior.
	deadline := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom
	if out.Decided && out.DecisionTime > deadline {
		out.Decided = false
		out.DecidedRound = -1
		for op, oo := range out.PerOp {
			if oo.Decided && oo.Time > deadline {
				oo.Decided = false
				oo.Round = -1
				if oo.Err == "" {
					oo.Err = "missed relay deadline"
				}
				out.PerOp[op] = oo
			}
		}
	}
	return out, nil
}

// desConfig is the QBFT-DES-internal configuration.
type desConfig struct {
	N            int
	BTT          time.Duration
	RT           time.Duration
	MaxRounds    int
	BFTStart     time.Duration
	Network      ct.NetworkModel
	Host         ct.HostPattern
	Byz          internalByz
	Seed         int64
	TraceEnabled bool
}

// rawOutcome is the QBFT-DES outcome before translation.
type rawOutcome struct {
	decided      bool
	decidedRound int
	decidedValue []byte
	decisionTime time.Duration
	perOp        map[ct.OperatorID]rawOpOutcome
	trace        []ct.TraceEntry
}

type rawOpOutcome struct {
	decided bool
	value   []byte
	round   int
	time    time.Duration
	err     string
}

func (r rawOutcome) toCT() ct.Outcome {
	// QBFT rounds are 1-indexed; framework's DecidedRound is 0-indexed
	// (matching OBFT's layer convention).
	normRound := func(qbftRound int) int {
		if qbftRound <= 0 {
			return -1
		}
		return qbftRound - 1
	}
	out := ct.Outcome{
		Decided:      r.decided,
		DecisionTime: r.decisionTime,
		DecidedValue: append([]byte(nil), r.decidedValue...),
		DecidedRound: normRound(r.decidedRound),
		PerOp:        make(map[ct.OperatorID]ct.OperatorOutcome, len(r.perOp)),
		Trace:        r.trace,
	}
	if !r.decided {
		out.DecidedRound = -1
	}
	for op, oo := range r.perOp {
		out.PerOp[op] = ct.OperatorOutcome{
			Decided: oo.decided,
			Value:   append([]byte(nil), oo.value...),
			Round:   normRound(oo.round),
			Time:    oo.time,
			Err:     oo.err,
		}
	}
	return out
}
