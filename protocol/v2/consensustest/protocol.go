// Package consensustest is a virtual-time discrete-event test framework
// for SSV consensus protocols. It defines an algorithm-agnostic Protocol
// interface so the same scenarios run through OBFT, QBFT, and future
// protocol-family members on apples-to-apples footing.
//
// Per-protocol adapters live under consensustest/{name}/ and translate the
// abstract ByzPattern into the protocol's internal byz model.
//
// Universal safety invariants — SingleV (≤ 1 V reconstructed cluster-wide),
// HonestAgreement, Terminated — are enforced on every simulation; SingleV
// and HonestAgreement violations panic.
package consensustest

import (
	"fmt"
	"time"
)

// OperatorID identifies a participant. IDs are 1..N; adapters convert as
// needed for their internal types.
type OperatorID uint64

// SimConfig is the algorithm-agnostic input to one simulation. Per-protocol
// adapters translate fields into their internal config. Timing is anchored at
// slot start (virtual-time 0); RelayCutoff is the hard deadline by which a
// signed output must be produced.
type SimConfig struct {
	N         int          // cluster size; F = (N-1)/3 is implied
	Operators []OperatorID // typically 1..N

	SlotStart    time.Duration // virtual-time offset of slot start; usually 0
	SlotDuration time.Duration // 12s for Ethereum
	RelayCutoff  time.Duration // application hard deadline (4s for proposer duty)

	// HeaderSubmitHeadroom is reserved between consensus completion and
	// RelayCutoff for cert broadcast + relay submit. 100ms at the operating point.
	HeaderSubmitHeadroom time.Duration

	// BTT (broadcast trip time) = network P99 one-way propagation + clock skew δ.
	// Protocols derive their per-phase windows from this (OBFT Δ_2 = 2 BTT;
	// QBFT round trip ≈ 1 BTT per message).
	BTT time.Duration

	Network NetworkModel
	Host    HostPattern

	// Byz translates to the protocol's internal byz model; kinds an adapter
	// can't faithfully translate cause it to return ErrNotApplicable.
	Byz ByzPattern

	Seed int64 // (Seed, SimConfig) → byte-identical event trace

	// TraceEnabled records every dispatched event into Outcome.Trace; default
	// off (turn on to replay an assertion failure).
	TraceEnabled bool

	// BLSKeys, when non-nil, switches the sim to real BLS for adapters that
	// support it. Generate with GenerateBLSKeys; reuse across sims.
	BLSKeys *BLSKeys
}

// Protocol is implemented by per-algorithm adapters. Adapters MUST be
// deterministic given (cfg, cfg.Seed) — two calls with the same input must
// produce byte-identical Outcome.Trace.
type Protocol interface {
	Name() string
	Run(cfg SimConfig) (Outcome, error)
}

// ErrNotApplicable is returned by Run when a scenario doesn't translate to
// this protocol (e.g. OBFT-specific h_V=1 on QBFT). The framework treats it
// as "skip" rather than "fail" when comparing outcomes.
var ErrNotApplicable = fmt.Errorf("scenario not applicable to this protocol")

// Outcome is the algorithm-agnostic per-sim result.
type Outcome struct {
	Decided      bool
	DecisionTime time.Duration // earliest cluster-wide successful decision; 0 if !Decided
	DecidedValue []byte        // protocol-opaque
	// DecidedRound is 0-indexed (OBFT layer or QBFT round-1); -1 if !Decided.
	DecidedRound int
	PerOp        map[OperatorID]OperatorOutcome
	Trace        []TraceEntry // non-nil iff cfg.TraceEnabled was set
}

type OperatorOutcome struct {
	Decided       bool
	Value         []byte
	Round         int // round / layer
	Time          time.Duration
	Err           string
	EvidenceCount int // protocol-specific count, opaque to the framework
}

type TraceEntry struct {
	When  time.Duration
	Event string
}

// BLSKeys is a threshold-shared BLS keypair. Adapters that support real BLS
// read this via SimConfig.BLSKeys.
type BLSKeys struct {
	ClusterPubKey []byte
	Shares        map[OperatorID][]byte // herumi-format secret shares
	PubShares     map[OperatorID][]byte // herumi-format public shares
}

// Validate sanity-checks the config and fills defaults.
func (c *SimConfig) Validate() error {
	if c.N < 4 {
		return fmt.Errorf("consensustest: N must be >= 4 (n = 3f+1 minimum)")
	}
	if (c.N-1)%3 != 0 {
		return fmt.Errorf("consensustest: N must be 3f+1 (got %d)", c.N)
	}
	if len(c.Operators) != c.N {
		return fmt.Errorf("consensustest: Operators length %d != N %d", len(c.Operators), c.N)
	}
	if c.BTT <= 0 {
		return fmt.Errorf("consensustest: BTT must be > 0")
	}
	if c.RelayCutoff <= 0 {
		return fmt.Errorf("consensustest: RelayCutoff must be > 0")
	}
	if c.SlotDuration <= 0 {
		return fmt.Errorf("consensustest: SlotDuration must be > 0")
	}
	if c.RelayCutoff > c.SlotDuration {
		return fmt.Errorf("consensustest: RelayCutoff (%v) > SlotDuration (%v)", c.RelayCutoff, c.SlotDuration)
	}
	if c.Network == nil {
		c.Network = ConstantDelay{D: c.BTT}
	}
	if c.Host == nil {
		c.Host = HostAllValid{}
	}
	return nil
}

// DefaultProposerDutyConfig returns a SimConfig at OBFT.md §Application's
// recommended proposer-duty operating point: n=4, RelayCutoff=4s. Pass `btt`
// to scale the operating point (200ms is the spec target; smaller stresses
// ideal mesh, larger stresses degraded mesh).
func DefaultProposerDutyConfig(btt time.Duration) SimConfig {
	operators := make([]OperatorID, 4)
	for i := range operators {
		operators[i] = OperatorID(i + 1)
	}
	return SimConfig{
		N:                    4,
		Operators:            operators,
		SlotStart:            0,
		SlotDuration:         12 * time.Second,
		RelayCutoff:          4 * time.Second,
		HeaderSubmitHeadroom: 100 * time.Millisecond,
		BTT:                  btt,
		Network:              ConstantDelay{D: btt},
		Host:                 HostAllValid{},
		Byz:                  ByzPattern{Kind: ByzNone},
		Seed:                 1,
	}
}
