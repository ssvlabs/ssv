package consensustest

// Phase identifies the protocol phase at which Validate is being asked. OBFT
// adapters pass PhasePhase1Acceptance once at bundle arrival (the verdict
// locks the operator's commitment to V; Phase-2 commit honors the locked
// verdict without re-querying). QBFT adapters pass PhaseDecide on every
// PROPOSE acceptance check. Adapters that don't care about phase semantics
// can ignore it.
type Phase int

const (
	PhasePhase1Acceptance Phase = iota // OBFT Phase 1 σ-emission acceptance window
	PhaseDecide                        // QBFT decide / generic single-call validation
)

// HostPattern returns the host's valid/not-valid verdict for (operator,
// layer, V, phase). Layer is the framework's 0-indexed layer/round:
//
//   - OBFT: layer ∈ [0, K-1] (the protocol layer the bundle was received at)
//   - QBFT: layer = QBFT round - 1, so round 1 maps to layer 0, round 2 to
//     layer 1, etc. — matching OBFT's "first opportunity = 0" convention.
//
// Phase distinguishes OBFT acceptance vs commit; see Phase docs.
type HostPattern interface {
	Validate(op OperatorID, layer int, v []byte, phase Phase) bool
}

// HostAllValid is the trivial all-valid host.
type HostAllValid struct{}

func (HostAllValid) Validate(_ OperatorID, _ int, _ []byte, _ Phase) bool { return true }

// HostInvalidForOperators returns false at the configured Layer for any
// op in Operators; useful for validity-divergence scenarios. Phase-agnostic.
//
// Operators is a lookup-only map (read via Operators[op] in Validate); not
// iterated, so map nondeterminism is not a concern. If you extend this type
// to iterate Operators, sort the keys or switch to a slice — Go map
// iteration order is randomized and would break sim determinism.
type HostInvalidForOperators struct {
	Layer     int
	Operators map[OperatorID]bool
}

func (h HostInvalidForOperators) Validate(op OperatorID, layer int, _ []byte, _ Phase) bool {
	if layer != h.Layer {
		return true
	}
	return !h.Operators[op]
}

// HostFlipMidSlot returns valid for layer ≤ ValidUntilLayer, not-valid for
// deeper layers. For OBFT, exercises the validate-once-and-lock property:
// ops accept V at L_0 (valid) and don't re-query the host at deeper layers,
// so the L_1+ "invalid" verdict is observed only via fall-through scenarios
// where the cluster needs to consider deeper layers. For QBFT, tests
// round-aware host validation: round 1 (= layer 0) PROPOSE accepted, round 2+
// PROPOSE rejected → cluster MISSES if round 1 doesn't decide.
type HostFlipMidSlot struct {
	ValidUntilLayer int
}

func (h HostFlipMidSlot) Validate(_ OperatorID, layer int, _ []byte, _ Phase) bool {
	return layer <= h.ValidUntilLayer
}

// HostInvalidUntilLayer returns not-valid for layer ≤ InvalidUntilLayer,
// valid for deeper layers. For OBFT, ops don't σ-emit at the invalidated
// layers; they NR there, NR-quorum unlocks the chain to a valid layer.
// For QBFT, round 1 PROPOSE rejected → round 2 fresh-V proposed → host
// validates round 2 (= layer 1) and slot decides via round-change.
type HostInvalidUntilLayer struct {
	InvalidUntilLayer int
}

func (h HostInvalidUntilLayer) Validate(_ OperatorID, layer int, _ []byte, _ Phase) bool {
	return layer > h.InvalidUntilLayer
}

// HostVerdictKey identifies one (op, layer, value_hash) verdict observation.
// Used by RecordingHostPattern to dedup-and-store the locked verdict per
// spec validate-once-and-lock semantics.
type HostVerdictKey struct {
	Op        OperatorID
	Layer     int
	ValueHash [32]byte
}

// RecordingHostPattern wraps an inner HostPattern, delegates Validate calls
// to it, and records each (op, layer, value_hash) → verdict observation in
// Verdicts. First-write-wins per key — captures the FIRST verdict each op
// produced for each (layer, V), matching the protocol's
// validate-once-and-lock contract (§Head-change handling). Subsequent
// re-evaluations of the same key (e.g., 2abOBFT Phase-2a re-validation)
// are silently dropped. Under a regression where the protocol acts on a
// re-evaluated flipped verdict instead of the locked first one, the
// recorded verdict still reflects the locked state — the Phase 2 C3
// safety check then catches the σ-emit-against-flipped-verdict regression.
//
// Used by the consensustest framework's Phase 2 C3 invariant
// (docs/CONSENSUSTEST-SAFETY-INVARIANTS-PLAN.md § Phase 2 — C1/C3
// follow-ups). The adapter wraps cfg.Host with this wrapper at Run()
// entry and passes the wrapper into the desCfg; computeAttestation
// post-sim cross-references Verdicts against
// Outcome.OfflineAgg.SigmaByEmitter at the decided (layer, V) to count
// honest emitters whose locked verdict was invalid.
//
// Map writes are unsynchronized — safe because the consensustest DES runs
// sequentially (event handlers don't execute concurrently). Production
// concurrent use would need a mutex.
type RecordingHostPattern struct {
	Inner    HostPattern
	Verdicts map[HostVerdictKey]bool
}

// NewRecordingHostPattern wraps inner with a verdict-recording delegate.
// The returned wrapper's Verdicts is non-nil; safe to read after Run()
// completes.
func NewRecordingHostPattern(inner HostPattern) *RecordingHostPattern {
	return &RecordingHostPattern{
		Inner:    inner,
		Verdicts: make(map[HostVerdictKey]bool),
	}
}

// Validate delegates to the inner host, records the verdict under
// first-write-wins semantics, and returns the inner's result unchanged
// (the wrapper is transparent to callers).
func (r *RecordingHostPattern) Validate(op OperatorID, layer int, v []byte, phase Phase) bool {
	verdict := r.Inner.Validate(op, layer, v, phase)
	key := HostVerdictKey{
		Op:        op,
		Layer:     layer,
		ValueHash: hashValue(v),
	}
	if _, exists := r.Verdicts[key]; !exists {
		r.Verdicts[key] = verdict
	}
	return verdict
}
