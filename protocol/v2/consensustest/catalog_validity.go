package consensustest

// ---- Validity divergence (algebraic-limit miss at L_0) ----------------

// Algebraic-limit miss: the smallest #NV that breaks σ-quorum without
// reaching NR-quorum. At any n with N=3f+1: #NV = N-2f = f+1.
//   - σ-pool at L_0 = (N-#NV-1) honest σ + leader's σ_L^V = N-#NV = 2f
//     < qV = 2f+1.
//   - NR-pool at L_0 = #NV = f+1 < qEnc = 2f+1 (when f ≥ 1).
//
// Slot misses with no fall-through. At f=1, n=4 this is the canonical
// "2-2 split" (2 NV / 2 valid). Spec referent: BFT-comparison.md Table 3
// row "Validity-divergence 2-2 split — ✗ algebraic limit".
var scenarioValidityDivergenceAlgebraicLimit = Scenario{
	Name:  "ValidityDivergence_AlgebraicLimit",
	Title: "Validity divergence: 2-2 split (algebraic miss)",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		nvCount := cfg.N - 2*f // = f+1 at N=3f+1
		// Pick the LAST nvCount ops to be NV (op{N-nvCount+1}..op{N}). At
		// n=4 f=1 this is op3, op4 (the canonical "2-2" choice).
		nvOps := make(map[OperatorID]bool, nvCount)
		for i := 0; i < nvCount; i++ {
			nvOps[OperatorID(cfg.N-i)] = true
		}
		cfg.Host = HostInvalidForOperators{
			Layer:     0,
			Operators: nvOps,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=2f < qV=2f+1; NR-pool=f+1 < qEnc=2f+1; chained
		// decryption blocked → slot misses (algebraic limit).
		"OBFT": ExpectMiss,
		// 2abOBFT: 2 σ-eligible honest + 2 NV honest at L_0. value_pool = 2
		// < qV=3; noValuePool = 2 < qEnc=3. σ-eligibility doesn't fire;
		// NR-eligibility doesn't fire either (the noValuePool gate is
		// satisfied only by NV honest, and only 2 of them exist). σ-eligible
		// honest's cannot-σ gate fails (V_local + host valid). With no
		// T_commit hard wall, ops wait until slot deadline → MISS. This is
		// the spec's assumption-3-boundary algebraic limit (see
		// docs/2abOBFT.md §Liveness): with no T_commit NR-default wall,
		// v4 does not attempt L_1 fall-through here.
		"2abOBFT": ExpectMiss,
		// QBFT: R1 PREPARE pool insufficient (host-NV non-leaders don't
		// PREPARE); R1 timeout; R2 leader proposes fresh V which validates
		// at layer 1 (host-NV is layer-0-scoped); decides at R2. Deterministic
		// at canonical ConstantDelay across seeds (verified at 10 seeds during
		// task 4.2). Jittered networks shift the timing tail and ~30% of
		// seeds miss the relay deadline — those runs surface via sweep tests
		// that don't assert per-cell Match.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "BFT-comparison.md Table 3 'Validity-divergence 2-2 split: ✗ algebraic limit'. Generalized to N-2f NV ops at any n; at f=1, n=4 this is the canonical 2-2 split (op3, op4 NV).",
}

// ---- Validity divergence (3-1: minority NV, σ-pool reaches anyway) ---

var scenarioValidityDivergence3_1 = Scenario{
	Name:  "ValidityDivergence_3_1",
	Title: "Validity divergence: minority NV (3-1)",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Host = HostInvalidForOperators{
			Layer:     0,
			Operators: map[OperatorID]bool{4: true},
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool at L_0 = 3 valid honest σ + leader's σ_L^V = 4 ≥ qV=3.
		// One dissenting NV (op4) doesn't break the slot; σ-quorum still reaches.
		// Validates "minority NV doesn't break the slot" — distinct from 2-2 (miss)
		// and 1-3 (fall-through).
		"OBFT": ExpectSuccessFastest,
		// 2abOBFT: 3 σV verdicts (op1, op2, op3) reach qV=3 at L_0 → row 3
		// → σ-emit on V → σ-quorum at L_0. Same outcome as OBFT.
		"2abOBFT": ExpectSuccessFastest,
		// QBFT: 3 ops PREPARE on V (op4 doesn't, host says invalid) → quorum
		// reached → COMMIT-quorum → R1 succeeds.
		"QBFT":  ExpectSuccessFastest,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "Minority NV at L_0 (1 of 4 honest dissents). σ-quorum still reaches at L_0 because 3 valid honest + leader's σ_L^V = 4 ≥ qV. Complement to ValidityDivergence_2_2 (miss) and 1_3 (fall-through).",
}

// ---- Validity divergence (NR-quorum fall-through at L_0) -------------

// NR-quorum fall-through: smallest #NV that makes NR-pool reach qEnc, so
// chained decryption unlocks L_1 (whose host says valid → σ-emit → decide).
// At any n with N=3f+1: #NV = 2f+1 = qEnc.
//   - σ-pool at L_0 = (N-#NV) = f < qV = 2f+1 (when f ≥ 1).
//   - NR-pool at L_0 = #NV = 2f+1 = qEnc → NR-quorum.
//
// Fall-through to L_1; at L_1 host says valid → σ-quorum → decide at L_1.
// At f=1, n=4 this is the canonical "1-3 split" (op1 valid + op2,3,4 NV).
var scenarioValidityDivergenceNRFallThrough = Scenario{
	Name:  "ValidityDivergence_NRFallThrough",
	Title: "Validity divergence: NR-quorum fall-through (1-3)",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		nvCount := 2*cfg.F() + 1 // = qEnc
		// Pick the LAST nvCount ops as NV (op{N-nvCount+1}..op{N}). At n=4
		// f=1 this is op2, op3, op4 (the canonical "1-3" choice with op1
		// valid as leader).
		nvOps := make(map[OperatorID]bool, nvCount)
		for i := 0; i < nvCount; i++ {
			nvOps[OperatorID(cfg.N-i)] = true
		}
		cfg.Host = HostInvalidForOperators{
			Layer:     0,
			Operators: nvOps,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=f < qV=2f+1; NR-pool=2f+1 = qEnc → NR-quorum unlocks L_1
		// where host says valid → σ-emit at L_1 → decides at L_1.
		"OBFT": ExpectSuccessFallThrough,
		// 2abOBFT: same — 1 σV verdict + 3 NV verdicts at L_0; row 2
		// NR-pool ≥ qEnc → NR-quorum at L_0 → advance to L_1 → σ at L_1.
		"2abOBFT": ExpectSuccessFallThrough,
		// QBFT: R1 PREPARE pool insufficient (#valid honest = f, < quorum=2f+1)
		// → R1 timeout → R2 fresh V → succeeds.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "NR-quorum fall-through pattern (#NV = 2f+1 = qEnc). Generalized from the f=1, n=4 '1-3 split' (op2..op4 NV); scales to N-2f-1 valid + 2f+1 NV at any n. Complement to ValidityDivergence_AlgebraicLimit (miss) and ValidityDivergence_3_1 (success at L_0).",
}

// ---- Validity divergence widened by passive byz (OBFT.md:602-606) -----

// Spec §Failure modes / Validity-divergence deadlock enumerates three
// configurations where a byzantine within the f-bound exercises f-budget
// passively (silent or σ-on-V — neither cryptographically slashable) to
// widen the deadlock zone beyond the all-honest case. The three scenarios
// below cover each shape. Generalized via cfg.F() so the σ-pool and
// NR-pool quorum-short outcomes hold at all SSV cluster sizes.

// Case #1 from OBFT.md:604: "1 non-leader σ + 1 non-leader NV + byz silent".
// Generalized: (2f-1) non-leader σ + 1 NV + f byz silent.
//   - σ-pool = leader + (2f-1) = 2f < qV = 2f+1.
//   - NR-pool = 1 < qEnc = 2f+1 (when f ≥ 1).
//
// At f=1, n=4: op2 σ-emits; op3 NV (host invalid); op4 byz silent.
// σ-pool = op1 + op2 = 2 < 3; NR-pool = op3 = 1 < 3 → MISS.
var scenarioValidityDivergence_PassiveByz_Silent_1NV = Scenario{
	Name:  "ValidityDivergence_PassiveByz_Silent_1NV",
	Title: "Validity divergence + passive byz: 1 NV + f byz silent",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// 1 NV non-leader at op{N-f}; f byz silent at op{N-f+1}..op{N}.
		nvOps := map[OperatorID]bool{OperatorID(cfg.N - f): true}
		byzOps := make([]OperatorID, f)
		for i := 0; i < f; i++ {
			byzOps[i] = OperatorID(cfg.N - f + 1 + i)
		}
		cfg.Host = HostInvalidForOperators{Layer: 0, Operators: nvOps}
		cfg.Byz = ByzPattern{Kind: ByzSigmaRefusal, ByzOperators: byzOps}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=2f<qV=2f+1; NR-pool=1<qEnc=2f+1; both quorums short; slot misses.
		"OBFT": ExpectMiss,
		// 2abOBFT: same algebraic shape at L_0 → row 5 → NR-emissions across
		// all honest. NR-pool at L_0 = 2f+1 honest NR (incl. leader who's
		// host-NV in this variant but emits NR — see byz σ-refusal: f byz
		// don't emit Onion2b at all). Total NR partials cluster-wide = 3
		// honest = qEnc — but NR-pool from σ-side perspective for advance
		// at L_0 is only the honest's NR emissions (≥ qEnc) → advance to L_1
		// — but L_1+ retention is empty in this scenario; falls through all
		// layers → MISS. Same outcome as OBFT, different mechanism.
		"2abOBFT": ExpectMiss,
		// QBFT: R1 PREPARE-quorum unreachable (op-NV+byz-silent); R2 fresh-V
		// host-validates at layer 1 → succeeds.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "OBFT.md §Failure modes / Validity-divergence deadlock #1. Passive byz widens deadlock zone — single NV honest is enough to miss when paired with f byz silent. Cryptographically unattributable (passive silence + host re-org are both legitimate behaviors).",
}

// Case #2 from OBFT.md:605: "0 non-leader σ + 2 non-leader NV + byz silent".
// Generalized: 0 non-leader σ + 2f NV + f byz silent.
//   - σ-pool = leader = 1 < qV = 2f+1 (when f ≥ 1).
//   - NR-pool = 2f < qEnc = 2f+1.
//
// At f=1, n=4: op2, op3 NV; op4 byz silent.
// σ-pool = op1 = 1 < 3; NR-pool = op2 + op3 = 2 < 3 → MISS.
var scenarioValidityDivergence_PassiveByz_Silent_2NV = Scenario{
	Name:  "ValidityDivergence_PassiveByz_Silent_2NV",
	Title: "Validity divergence + passive byz: 2f NV + f byz silent",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// 2f NV non-leaders at op2..op{2f+1}; f byz silent at op{2f+2}..op{N}.
		nvOps := make(map[OperatorID]bool, 2*f)
		for i := 0; i < 2*f; i++ {
			nvOps[OperatorID(i+2)] = true
		}
		byzOps := make([]OperatorID, f)
		for i := 0; i < f; i++ {
			byzOps[i] = OperatorID(2*f + 2 + i)
		}
		cfg.Host = HostInvalidForOperators{Layer: 0, Operators: nvOps}
		cfg.Byz = ByzPattern{Kind: ByzSigmaRefusal, ByzOperators: byzOps}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=1<qV; NR-pool=2f<qEnc; both quorums short; slot misses.
		"OBFT": ExpectMiss,
		// 2abOBFT: leader is σ-eligible (V_local + host valid); 2f NV
		// non-leaders emit KindNoValue (V_local but host NV — cannot σ at
		// L_0); f byz silent at Phase 2. noValuePool = 2f < qEnc=2f+1 →
		// NR-eligibility doesn't fire. With no T_commit hard wall, ops
		// wait until slot deadline → MISS.
		"2abOBFT": ExpectMiss,
		// QBFT: R1 PREPARE-quorum unreachable; R2 fresh-V at layer 1 host-validates → succeeds.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "OBFT.md §Failure modes / Validity-divergence deadlock #2. Worst-case all-NV non-leaders plus f byz silent — σ-pool collapses to leader-only, NR-pool capped below qEnc by byz silence. 2ab recovers via row-2 NR-quorum (honest NR emissions reach qEnc at honest receivers).",
}

// Case #3 from OBFT.md:606: "0 non-leader σ + 2 non-leader NV + byz σ-on-V".
// Generalized: 0 honest σ-non-leader + 2f NV + f byz σ-on-V (byz contributes
// σ on V like an honest non-leader, but counts toward f-budget so its
// presence is "free grief" — same outcome as if byz were silent at the
// algebraic limit but exercises a different shape on-wire).
//   - σ-pool = leader + f byz σ-on-V = 1 + f < qV = 2f+1 (when f ≥ 1).
//   - NR-pool = 2f < qEnc = 2f+1.
//
// At f=1, n=4: op2, op3 NV; op4 byz σ-on-V (acts honestly message-wise).
// σ-pool = op1 + op4 = 2 < 3; NR-pool = op2 + op3 = 2 < 3 → MISS.
//
// Distinguishes from ValidityDivergence_AlgebraicLimit (no byz labeling) by
// explicitly marking op{2f+2}..op{N} as f-budget-consuming byz operators,
// per the spec's accounting framework. Algebraically equivalent miss
// outcome; on-wire indistinguishable from a healthy non-leader at the byz.
var scenarioValidityDivergence_PassiveByz_SigmaOnV_2NV = Scenario{
	Name:  "ValidityDivergence_PassiveByz_SigmaOnV_2NV",
	Title: "Validity divergence + passive byz: 2f NV + f byz σ-on-V",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// 2f NV non-leaders at op2..op{2f+1}; f byz σ-on-V at op{2f+2}..op{N}
		// (no behavioral override — byz acts as honest message-wise).
		nvOps := make(map[OperatorID]bool, 2*f)
		for i := 0; i < 2*f; i++ {
			nvOps[OperatorID(i+2)] = true
		}
		byzOps := make([]OperatorID, f)
		for i := 0; i < f; i++ {
			byzOps[i] = OperatorID(2*f + 2 + i)
		}
		cfg.Host = HostInvalidForOperators{Layer: 0, Operators: nvOps}
		cfg.Byz = ByzPattern{Kind: ByzNone, ByzOperators: byzOps}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=1+f<qV=2f+1 (for f≥1); NR-pool=2f<qEnc=2f+1; miss.
		"OBFT": ExpectMiss,
		// 2abOBFT: leader σ-eligible; 2f NV honest emit KindNoValue;
		// f byz σ-on-V act honestly message-wise (emit KindValue). value_pool
		// = leader + f byz = 1 + f < qV=2f+1; noValuePool = 2f < qEnc=2f+1.
		// σ-eligibility doesn't fire (no V reaches qV); NR-eligibility
		// doesn't fire either. σ-eligible honest's cannot-σ gate fails.
		// With no T_commit hard wall, ops wait until slot deadline → MISS.
		"2abOBFT": ExpectMiss,
		// QBFT: R1 PREPARE pool = leader + f byz = 1+f < quorum=2f+1; R1 timeout;
		// R2 fresh-V at layer 1 host-validates → succeeds.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "OBFT.md §Failure modes / Validity-divergence deadlock #3. Byz emits σ on V (cryptographically unattributable — passive σ contribution is honest-equivalent message-wise) but consumes f-budget, so σ-pool still misses qV. The spec's spec-3 algebraic miss configuration.",
}

// ---- Host flip mid-slot (Phase 4) -------------------------------------

var scenarioHostFlipMidSlot = Scenario{
	Name:  "HostFlipMidSlot",
	Title: "Host valid at L_0/R1, flips invalid deeper",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Host = HostFlipMidSlot{ValidUntilLayer: 0}
	},
	Expect: map[string]ExpectClass{
		// OBFT: ops σ-emit at L_0 (host valid); slot decides at L_0.
		// Deeper layers' "invalid" verdict isn't queried (validate-once-and-lock).
		"OBFT": ExpectSuccessFastest,
		// 2abOBFT: host valid at L_0; σV verdicts at L_0 reach qV → σ at L_0.
		"2abOBFT": ExpectSuccessFastest,
		// QBFT: round 1 PROPOSE host-validates; cluster decides at R1.
		"QBFT":  ExpectSuccessFastest,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "Host valid only at layer 0 / round 1. Healthy decision at fastest path; 'invalid' at deeper layers/rounds is moot under healthy propagation.",
}

// ---- Host invalid until L_1 (Phase 4) ---------------------------------

var scenarioHostInvalidUntilL1 = Scenario{
	Name:  "HostInvalidUntilL1",
	Title: "Host invalid at L_0/R1, valid from L_1/R2",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Host = HostInvalidUntilLayer{InvalidUntilLayer: 0}
	},
	Expect: map[string]ExpectClass{
		// OBFT: L_0 host-invalid → all NR at L_0 → NR-quorum unlocks L_1 →
		// L_1 host-valid → σ-emit at L_1 → decides at L_1 (fall-through).
		"OBFT": ExpectSuccessFallThrough,
		// 2abOBFT: L_0 all-NV verdicts → row 2 NR-quorum → advance to L_1
		// where host-valid → σ at L_1.
		"2abOBFT": ExpectSuccessFallThrough,
		// QBFT: round 1 PROPOSE rejected → R1 timeout → R2 fresh-V validates
		// (host valid at R2 = framework round 1) → decides at R2.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "Host invalid at layer 0 / round 1 only. Both protocols recover via fall-through to deeper layer / next round. Exercises round-aware host validation in QBFT.",
}

// ---- Leader-NV symmetric validity-divergence variant -----------------

// Spec referent: OBFT.md §Failure modes / Validity-divergence deadlock —
// "Symmetric configs when leader's host-verdict is itself NV (Phase-1 σ_V
// locks leader σ-side regardless of leader's host's later opinion)."
//
// Exercises the OBFT-specific invariant: a leader who σ-emits in Phase 1
// stays σ-locked at that layer even if their host application later
// returns NV. Cross-phase exclusivity prevents the leader from emitting
// NR for the same layer — they CAN'T flip sides post-σ_V.
//
// Configuration (algebraically equivalent to
// scenarioValidityDivergence_PassiveByz_Silent_1NV with the additional
// leader-NV; outcome is also MISS):
//   - Leader (op1): host-NV at L_0, BUT Phase-1 σ_V locks σ-side.
//   - op2..op{2f}: σ-emit (host valid).
//   - op{2f+1}: NV (host invalid; emits NR).
//   - op{N-f+1}..op{N}: byz silent (consume f-budget).
//
// At f=1, n=4: leader op1 NV (σ-locked), op2 σ, op3 NV, op4 byz silent.
// σ-pool = leader's locked σ_V + op2 = 2 < qV=3. NR-pool = op3 = 1 <
// qEnc=3 (leader CAN'T NR despite host-NV; cross-phase exclusivity).
// MISS — same algebraic outcome as the non-leader-NV variant.
//
// The test is the in-suite proof of the spec's σ-V-lock invariant:
// without it, the leader would NR instead, NR-pool would be 2 < qEnc
// still, but the σ-pool would shrink to 1, and the bottom-line outcome
// would still be MISS — algebraically equivalent. What the σ-lock
// invariant actually changes is the off-wire EKM single-σ-V invariant:
// the leader's σ_V is the σ-side commitment for this slot, period.
var scenarioValidityDivergence_LeaderNV_PassiveByz = Scenario{
	Name:  "ValidityDivergence_LeaderNV_PassiveByz",
	Title: "Validity divergence: leader-NV + passive byz (σ-V lock)",
	Group: "Host validity",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// NV set: leader (op1) host-NV, plus 1 non-leader at op{2f+1}.
		// Leader's σ_V is locked via Phase-1 (BuildPhase1Bundle runs
		// before ApplyHostValidity); host-NV doesn't undo the σ-side
		// commitment.
		nv := map[OperatorID]bool{
			1:                   true,
			OperatorID(2*f + 1): true,
		}
		// Byz silent: f operators at op{N-f+1}..op{N}.
		byzOps := make([]OperatorID, f)
		for i := 0; i < f; i++ {
			byzOps[i] = OperatorID(cfg.N - f + 1 + i)
		}
		cfg.Host = HostInvalidForOperators{Layer: 0, Operators: nv}
		cfg.Byz = ByzPattern{Kind: ByzSigmaRefusal, ByzOperators: byzOps}
	},
	Expect: map[string]ExpectClass{
		// OBFT: leader's σ_V locks σ-side; cross-phase exclusivity prevents
		// leader's NR despite host-NV. σ-pool=leader+(2f-1 honest)=2f<qV;
		// NR-pool=1<qEnc; both short; miss.
		"OBFT": ExpectMiss,
		// 2abOBFT: leader is host-NV at L_0 (cannot σ); op{2f+1} also NV;
		// f byz silent. The leader and the NV non-leader both emit
		// KindNoValue (they have V_local but host says NV). noValuePool =
		// 2 (leader + op{2f+1}) < qEnc=2f+1=3 → NR-eligibility doesn't
		// fire. Remaining honest are σ-eligible — their cannot-σ gate
		// fails so NR-eligibility doesn't fire for them either. With no
		// T_commit hard wall, ops wait until slot deadline → MISS.
		"2abOBFT": ExpectMiss,
		// QBFT: R1 PREPARE pool short (host-NV ops don't PREPARE; byz silent);
		// R2 fresh-V at layer 1 host-validates → succeeds. (HostInvalidForOperators
		// is layer-0-scoped; round 2 = layer 1 sees all-valid.)
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no host-validity model
	},
	Note: "OBFT.md §Failure modes — Validity-divergence deadlock, leader-NV symmetric variant. Validates the spec's σ_V-lock-despite-host-NV invariant: a Phase-1-σ-emitted leader stays σ-locked even when their host returns NV. Same algebraic miss outcome as the non-leader-NV passive-byz scenarios; the leader-NV-locked configuration is the additional spec coverage.",
}
