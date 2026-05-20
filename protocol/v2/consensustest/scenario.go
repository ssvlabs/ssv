package consensustest

import (
	"fmt"
	"strings"
	"time"
)

// Scenario describes a test condition independently of the protocol under
// test. Apply modifies SimConfig (typically Byz / Host / Network); Expect
// declares per-protocol outcome buckets; Modes opts the scenario into the
// correctness and/or stress tiers (see docs/CONSENSUSTEST-SPLIT-PLAN.md).
type Scenario struct {
	Name  string
	Title string // human-readable label for reports/charts; falls back to Name when empty
	Group string // taxonomic bucket for report grouping (e.g. "Silent operators"); empty = "Other"

	// Modes lists the tiers this scenario participates in. An empty slice
	// is treated as {ModeStress} for back-compat — every Catalog scenario
	// is currently a stress contributor; correctness opt-ins are filled in
	// during the per-scenario audit (Phase 2 of the split plan).
	Modes []Mode

	// Delivery selects the transport model for this scenario. Default
	// DeliveryDirect (full fanout); set DeliveryMesh on baseline /
	// healthy scenarios that should run through the mesh transport.
	// Adversarial / propagation / selective-broadcast scenarios should
	// stay on DeliveryDirect — their per-(from, to) primitives lose
	// meaning under mesh reflood. Applied to SimConfig.Delivery before
	// Apply runs (see ApplyScenario).
	Delivery DeliveryMode

	Apply func(*SimConfig)

	// Applies, when non-nil, is consulted by the runner BEFORE Apply runs.
	// Returning false causes the sim to be skipped with ErrNotApplicable
	// — the cell renders as n/a, no Apply / Run is invoked, and Fields-
	// tuple attribution remains truthful (the cell isn't attributed
	// data from a different operating point). Use this for scenarios
	// that are only meaningful at a subset of the sweep's (n, K, ...)
	// matrix, e.g. MultiSilent_AllLayers requires K==N to silence every
	// operator-leader; at K<N the scenario would internally override
	// cfg.K=N and the cell's recorded Fields["K"] would lie.
	//
	// The check sees cfg PRE-Apply (sweep-supplied), so Applies can read
	// whatever the sweep selected without surprises from Apply mutations.
	Applies func(SimConfig) bool

	// Expect is keyed by Protocol.Name(). Use ExpectFor(name) for lookup
	// rather than direct map access — it falls back from variant names
	// (e.g. "QBFT-SSV") to the base name (e.g. "QBFT") so variants don't
	// each need their own entry unless they expect a different outcome.
	Expect map[string]ExpectClass
	Note   string // doc pointer (BFT-comparison.md row, OBFT.md section, ...)
}

// CloneScenarioWith returns a copy of `s` whose Apply runs `s.Apply`
// (if non-nil) first and then `extra` (if non-nil). Every other field
// — including Delivery — is carried through unchanged. Use this in
// sweep builders that need to wrap a scenario's Apply with axis-specific
// composition (loss model, correlated-link wrap, etc.) so the cloned
// scenario can never silently drop a field; field-by-field struct
// rebuilds were the source of a real regression where Healthy in
// packet-loss / correlated-delay / node-slowness sweeps reverted from
// DeliveryMesh to DeliveryDirect because the rebuild forgot the new
// Delivery field.
func CloneScenarioWith(s Scenario, extra func(*SimConfig)) Scenario {
	inner := s
	out := s
	out.Apply = func(cfg *SimConfig) {
		if inner.Apply != nil {
			inner.Apply(cfg)
		}
		if extra != nil {
			extra(cfg)
		}
	}
	return out
}

// ExpectFor looks up the declared expectation for protocol name `pname`,
// with variant fallback so family variants don't each need their own
// Expect-map entry. Lookup order:
//
//  1. exact match on pname
//  2. fallback to the protocol's "base" name (canonical family),
//     determined by stripping either of the two variant-suffix
//     conventions: "xN" multiplier suffix ("OBFTx2" → "OBFT") or
//     "-suffix" variant suffix ("QBFT-SSV" → "QBFT")
//
// Scenarios that legitimately need a different expectation for a
// variant (e.g. QBFT-SSV's tighter fixed-RT misses where QBFT's
// computed-RT succeeds; OBFTx3's T_commit collapse at a BTT where
// OBFT succeeds) can override by setting both keys explicitly.
// Returns (zero, false) when neither key is present.
func (s Scenario) ExpectFor(pname string) (ExpectClass, bool) {
	if v, ok := s.Expect[pname]; ok {
		return v, true
	}
	if base := variantBase(pname); base != pname {
		if v, ok := s.Expect[base]; ok {
			return v, true
		}
	}
	return 0, false
}

// variantBase returns the canonical family name for a protocol-variant
// name. Strips the two suffix conventions the framework uses:
//   - "xN" multiplier suffix (e.g. "OBFTx2" → "OBFT", "2abOBFTx3" →
//     "2abOBFT") where N is one or more trailing digits preceded by 'x'
//   - "-suffix" variant flavor (e.g. "QBFT-SSV" → "QBFT")
//
// Returns the input unchanged when neither suffix is present.
func variantBase(name string) string {
	// "xN" suffix: walk back from the end while the byte is a digit;
	// if at least one digit was consumed and the byte before is 'x',
	// strip both.
	digitStart := len(name)
	for digitStart > 0 && name[digitStart-1] >= '0' && name[digitStart-1] <= '9' {
		digitStart--
	}
	if digitStart < len(name) && digitStart > 0 && name[digitStart-1] == 'x' {
		return name[:digitStart-1]
	}
	// "-suffix" suffix.
	if i := strings.IndexByte(name, '-'); i > 0 {
		return name[:i]
	}
	return name
}

// IsAdversarial reports whether the scenario requires active byzantine
// behavior to reproduce (cfg.Byz.Kind != ByzNone after Apply). Honest-
// cluster failure modes — pure-network slowness, host validity issues,
// passive-byz scenarios — return false. Auto-detected via probe runs
// of Apply against minimal SimConfigs so adding a new scenario doesn't
// require remembering to flag it.
//
// Used by the UI to visually distinguish "happens in production with
// honest nodes" from "requires an attacker".
//
// Probe runs at every supported cluster size (ClusterSizes) and ORs the
// verdicts so a scenario whose Apply only ENGAGES byz at N>4 — none
// today, but plausible if a future scenario scales adversarial activity
// with cfg.F() and turns dormant at f=1 — still reads as adversarial.
// The cross-N OR is cheap (a handful of Apply calls, no sim runs) and
// removes the latent footgun.
func (s Scenario) IsAdversarial() bool {
	if s.Apply == nil {
		return false
	}
	for _, n := range ClusterSizes {
		probe := SimConfig{
			N:         n,
			Operators: MakeOperators(n),
			K:         n,
			// BTT is set to a realistic value so scenarios whose Apply
			// scales per-receiver overrides off cfg.BTT (e.g.
			// MeshFlakiness's 2·BTT) don't accidentally trip over a
			// degenerate near-zero override. The probe doesn't run a
			// sim so this only affects what gets recorded into the
			// probe SimConfig; the IsAdversarial verdict cares only
			// about probe.Byz.Kind.
			BTT: 200 * time.Millisecond,
		}
		s.Apply(&probe)
		// ByzMultiSilent is topology-driven (silences whichever
		// operator leads the top K layers — see byz.go's ByzPattern
		// doc); it doesn't require an attacker. Treat it as a non-
		// adversarial failure mode so the UI doesn't flag it as
		// "needs byz" in the heatmap legend.
		if probe.Byz.Kind != ByzNone && probe.Byz.Kind != ByzMultiSilent {
			return true
		}
	}
	return false
}

// HasMode reports whether the scenario participates in mode m. An empty
// Modes slice is treated as {ModeStress} so unannotated Catalog entries
// keep flowing into the existing stress report unchanged.
func (s Scenario) HasMode(m Mode) bool {
	if len(s.Modes) == 0 {
		return m == ModeStress
	}
	for _, x := range s.Modes {
		if x == m {
			return true
		}
	}
	return false
}

// ScenariosWithMode filters a scenario slice to those opted into mode m.
// Used by tier entry points (TestCorrectness, TestStress) to derive their
// scenario lists from Catalog.
func ScenariosWithMode(scenarios []Scenario, m Mode) []Scenario {
	out := make([]Scenario, 0, len(scenarios))
	for _, s := range scenarios {
		if s.HasMode(m) {
			out = append(out, s)
		}
	}
	return out
}

// DisplayTitle returns the scenario's Title if set, otherwise Name. Used
// by report renderers for human-readable chart/section labels.
func (s Scenario) DisplayTitle() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Name
}

// ExpectClass enumerates the canonical outcome buckets.
type ExpectClass int

const (
	ExpectSuccessFastest     ExpectClass = iota // round/layer 0 (Healthy)
	ExpectSuccessFallThrough                    // deeper round/layer
	ExpectMiss                                  // clean slot miss, no safety violation
	ExpectNotApplicable                         // scenario doesn't translate to this protocol
	ExpectSuccessOrMiss                         // outcome timing-dependent; either accepted
)

func (e ExpectClass) String() string {
	switch e {
	case ExpectSuccessFastest:
		return "success/fastest"
	case ExpectSuccessFallThrough:
		return "success/fall-through"
	case ExpectMiss:
		return "miss"
	case ExpectNotApplicable:
		return "n/a"
	case ExpectSuccessOrMiss:
		return "success-or-miss"
	default:
		return fmt.Sprintf("unknown(%d)", int(e))
	}
}

// Match reports whether outcome `o` matches `expect`, plus a rationale on
// mismatch. Reaching the ExpectNotApplicable case here means the adapter ran
// despite the scenario being declared n/a — a translation bug in the adapter.
func Match(o Outcome, expect ExpectClass) (ok bool, why string) {
	switch expect {
	case ExpectSuccessFastest:
		if !o.Decided {
			return false, "expected success at fastest path; got MISS"
		}
		if o.DecidedRound != 0 {
			return false, fmt.Sprintf("expected fastest path (round/layer 0); got %d", o.DecidedRound)
		}
		return true, ""
	case ExpectSuccessFallThrough:
		if !o.Decided {
			return false, "expected success via fall-through; got MISS"
		}
		if o.DecidedRound == 0 {
			return false, "expected deeper round/layer (>=1); got 0"
		}
		return true, ""
	case ExpectMiss:
		if o.Decided {
			return false, fmt.Sprintf("expected MISS; got success at round %d", o.DecidedRound)
		}
		return true, ""
	case ExpectNotApplicable:
		return false, "protocol returned an Outcome but scenario was declared not applicable"
	case ExpectSuccessOrMiss:
		return true, ""
	default:
		return false, fmt.Sprintf("unknown expect class %d", int(expect))
	}
}
