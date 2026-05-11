package consensustest

import "time"

// Catalog is the canonical list of cross-protocol scenarios. Each Scenario
// declares a SimConfig modifier and per-protocol expectations sourced from
// docs/BFT-comparison.md and docs/OBFT.md §Application.
//
// Scenarios are ordered roughly by failure-mode taxonomy: healthy → silent
// leader → multi-silent → equivocation patterns → validity divergence →
// OBFT-specific patterns (n/a for QBFT).
//
// Add new scenarios at the end to keep order stable for matrix-rendering.
//
// Cluster-size scope: the matrix runs at n=4 (the canonical operating
// point per OBFT.md §Application). The larger-n sweep
// (TestSweep_FullCatalog_LargerN in sweep_test.go) runs every scenario at
// n ∈ {7, 10, 13} too, enforcing universal safety invariants and asserting
// per-cell expectation matches. Every catalog scenario produces the same
// outcome class at all SSV cluster sizes — Apply functions scale with
// cfg.N / cfg.F() so f-quorum mechanics are preserved.
//
// Generalization key: scenarios named with a number suffix (e.g. "_3_1",
// "_AlgebraicLimit", "_NRFallThrough", "_NaturalRecovery") describe the
// f=1 / n=4 instance of a behavior class. The Apply scales the count of
// affected operators (NV count, recipient count) with cfg.F() so the
// behavior class holds at any n.
var Catalog = []Scenario{
	// Baseline
	scenarioHealthy,

	// Silent operators
	scenarioSilentLeaderL0,
	scenarioMultiSilent,
	scenarioMultiSilent_AllLayers,
	scenarioSigmaRefusal,
	scenarioWithholdLeaderDeepest,
	scenarioCertWithholding,

	// Leader equivocation
	scenarioEquivocate111,
	scenarioEquivocateAllNR,
	scenarioEquivocateSigmaLockedSplit,
	scenarioPartialEquivocationNaturalRecovery,

	// Host validity
	scenarioValidityDivergence3_1,
	scenarioValidityDivergenceAlgebraicLimit,
	scenarioValidityDivergenceNRFallThrough,
	scenarioValidityDivergence_PassiveByz_Silent_1NV,
	scenarioValidityDivergence_PassiveByz_Silent_2NV,
	scenarioValidityDivergence_PassiveByz_SigmaOnV_2NV,
	scenarioValidityDivergence_LeaderNV_PassiveByz,
	scenarioHostInvalidUntilL1,
	scenarioHostFlipMidSlot,

	// Propagation issues
	scenarioHV1SelectiveDelivery,
	scenarioLateLeaderBroadcast,
	scenarioAsymmetricPropagation_FSlow_Success,
	scenarioAsymmetricPropagation_FPlus1Slow_Miss,
	scenarioMeshFlakiness,

	// OBFT-specific attacks
	scenarioCrossSigningRule1,
	scenarioCrossOnionEquivocationRule3,
	scenarioFakeEncryptedPresence,
	scenarioFakePlaintextSigmaRule5,
}

// ---- Healthy ------------------------------------------------------------

var scenarioHealthy = Scenario{
	Name:  "Healthy",
	Title: "All-honest healthy path",
	Group: "Baseline",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzNone}
		cfg.Host = HostAllValid{}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFastest,
		"QBFT": ExpectSuccessFastest,
	},
	Note: "Both protocols complete at fastest path under all-honest, in-budget propagation. BFT-comparison.md Table 1.",
}

// ---- Silent primary leader ---------------------------------------------

var scenarioSilentLeaderL0 = Scenario{
	Name:  "PrimaryLeaderSilent",
	Title: "Primary leader silent",
	Group: "Silent operators",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzSilentLeader, ByzOperators: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFallThrough, // V_0 silent → in-round to V_1
		"QBFT": ExpectSuccessFallThrough, // R1 silent → R2 success
	},
	Note: "Primary leader silent. OBFT falls through K-layer in-round; QBFT round-changes to R2 (pays RT timeout).",
}

// ---- Multi-silent (top 3 of 4 leaders silent) --------------------------

var scenarioMultiSilent = Scenario{
	Name:  "MultiSilent_K3",
	Title: "Top K-1 leaders silent (deepest is honest)",
	Group: "Silent operators",
	Apply: func(cfg *SimConfig) {
		// Top 3 leaders silent; only the deepest is honest.
		cfg.Byz = ByzPattern{Kind: ByzMultiSilent, K: 3}
	},
	Expect: map[string]ExpectClass{
		// OBFT recovers in-round via K-layer fall-through to L_3.
		"OBFT": ExpectSuccessFallThrough,
		// QBFT needs 3 round-changes (R1, R2, R3 all silent → R4 honest).
		// At RT=2s × 3 timeouts = 6s, exceeds RelayCutoff=4s → MISS.
		"QBFT": ExpectMiss,
	},
	Note: "Top 3 of 4 leaders silent; only the deepest is honest. Structural OBFT-family advantage at any (BFT_start, D) where the healthy path fits — multi-leader-silent fall-through is in-round, vs QBFT's serial round-change exceeding budget.",
}

// ---- Leader equivocates 1-1-1 (all-distinct at f=1) -------------------

// Historical name "1-1-1" reflects the f=1 / n=4 case (V_1 → A, V_2 → B,
// V_3 → C). The byzEquivoc111 pattern actually emits N-1 distinct V's at
// any cluster size — one per honest receiver. The σ-locked-split slot-miss
// outcome holds at all n: each honest σ-locks on their own distinct V
// before observing equivocation; σ-pool on each V_i = 1 + leader's σ_L^V =
// 2 < qV = 2f+1; no fall-through (cross-phase exclusivity locks σ-locked
// honest out of NR).
var scenarioEquivocate111 = Scenario{
	Name:  "Equivocate_111",
	Title: "Leader equivocates: N-1 distinct values",
	Group: "Leader equivocation",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzEquivocate111, ByzOperators: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pools split below qV; no NR-quorum; slot misses.
		"OBFT": ExpectMiss,
		// QBFT: PREPARE pool fragments; R1 timeout → R2 with fresh V → success.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "BFT-comparison.md Table 3: QBFT recovers via R2 fresh-V; OBFT misses (R-invariant). Pattern emits N-1 distinct V's at any cluster size; the '1-1-1' name reflects f=1.",
}

// ---- Leader equivocates all-NR (floods both V's to all honest) --------

var scenarioEquivocateAllNR = Scenario{
	Name:  "Equivocate_AllNR",
	Title: "Leader equivocates: floods both values to all",
	Group: "Leader equivocation",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzEquivocateAllNR, ByzOperators: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		// OBFT: every honest retains ≥ 2 V's, NRs per equivocation rule;
		// NR-quorum at L_0 → fall-through to L_1.
		"OBFT": ExpectSuccessFallThrough,
		// QBFT: byz proposer splits PROPOSE delivery 50/50 (V_a to half,
		// V_b to half); receivers register only the first PROPOSE for the
		// (signer, round) pair (AddFirstMsgForSignerAndRound dedups
		// silently — no equivocation detection here, unlike OBFT's witness
		// re-hydration). PREPARE pool fragments across V_a/V_b; R1 timeout
		// → R2 with fresh V → success.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "Both recover at fall-through cost; OBFT in-round (cheap, equivocation-NR-driven), QBFT pays RT (expensive, PREPARE-pool-fragmentation-driven).",
}

// ---- σ-locked split (f-f) ---------------------------------------------

// Generalized form: byz delivers V_a to f honest, V_b to f honest, ∅ to the
// remaining N-1-2f honest. At any n: σ-pool on each V = f + leader's σ_L^V =
// f+1 < qV (since qV = 2f+1); NR-pool from silent rest = N-1-2f = f at
// N=3f+1, < qEnc → MISS at L_0 with no fall-through. Historical name "1-1"
// reflects f=1 / n=4; generalized to "f-f" preserves the σ-locked-split
// slot-miss class at all SSV cluster sizes.
var scenarioEquivocateSigmaLockedSplit = Scenario{
	Name:  "Equivocate_SigmaLockedSplit",
	Title: "Leader equivocates: σ-locked f-f split",
	Group: "Leader equivocation",
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// First f recipients receive V_a, next f receive V_b. op2..op{f+1}
		// for V_a, op{f+2}..op{2f+1} for V_b.
		recipients := make([]OperatorID, 0, 2*f)
		for i := 0; i < 2*f; i++ {
			recipients = append(recipients, OperatorID(i+2))
		}
		cfg.Byz = ByzPattern{
			Kind:         ByzEquivocateSigmaLockedSplit,
			ByzOperators: []OperatorID{1},
			Recipients:   recipients,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool on each V = f+1 < qV=2f+1; NR-pool from silent rest = f
		// < qEnc=2f+1; slot misses.
		"OBFT": ExpectMiss,
		// QBFT: PREPARE pool splits; R1 timeout → R2 fresh V → success.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "OBFT-family R-invariant slot-miss at any cluster size (generalized 1-1 → f-f); QBFT R2 recovers.",
}

// ---- h_V=f selective delivery (OBFT-specific) -------------------------

// Generalized form of h_V=1: byz delivers V to exactly f honest. At any n:
//   - σ-pool = f honest σ + leader's σ_L^V = f+1 < qV (since qV = 2f+1)
//   - NR-pool = (N-1-f) silent honest = (N-1-f); cross-checking N=3f+1:
//     N-1-f = 3f+1-1-f = 2f < qEnc (since qEnc = 2f+1)
//
// → MISS at L_0 with no fall-through, at any cluster size.
//
// Historical name "HV1Selective" reflects the f=1 / n=4 case; the pattern
// is named for behavior class, not recipient count.
var scenarioHV1SelectiveDelivery = Scenario{
	Name:  "HV1SelectiveDelivery",
	Title: "Selective delivery: V to f honest only",
	Group: "Propagation issues",
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		recipients := make([]OperatorID, 0, f)
		// Pick op2..op{f+1} as the f honest that receive V (op1 is byz leader).
		for i := 0; i < f; i++ {
			recipients = append(recipients, OperatorID(i+2))
		}
		cfg.Byz = ByzPattern{
			Kind:         ByzHV1SelectiveDelivery,
			ByzOperators: []OperatorID{1},
			Recipients:   recipients,
		}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectMiss,
		"QBFT": ExpectNotApplicable,
	},
	Note: "OBFT-specific deadlock pattern (h_V=f at any cluster size). σ-pool=f+1 < qV; NR-pool=2f < qEnc → MISS at L_0 with no fall-through. QBFT's round-change recovers structurally; pattern doesn't translate.",
}

// ---- Fake encrypted presence (OBFT-specific Rule 4) -------------------

var scenarioFakeEncryptedPresence = Scenario{
	Name:  "FakeEncryptedPresence",
	Title: "Forged encrypted-presence at L_1 (Rule 4)",
	Group: "OBFT-specific attacks",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:         ByzFakeEncryptedPresence,
			ByzOperators: []OperatorID{1},
			Layer:        1,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: byz silent at L_0 → NR-quorum unlocks L_1 decryption →
		// honest L_1 leader's bundle reconstructs at L_1; byz's L_1
		// garbage triggers Rule 4 evidence (recorded but not asserted here
		// — adapter tracks via PerOp.EvidenceByRule).
		"OBFT": ExpectSuccessFallThrough,
		"QBFT": ExpectNotApplicable,
	},
	Note: "OBFT-specific (no chained encryption in QBFT). Verifies Rule 4 detection path under real-BLS or stub-IBE.",
}

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
		// QBFT: R1 PREPARE pool insufficient (host-NV non-leaders don't
		// PREPARE); R1 timeout; R2 leader proposes fresh V which validates
		// at layer 1 (host-NV is layer-0-scoped); decides at R2. Deterministic
		// at canonical ConstantDelay across seeds (verified at 10 seeds during
		// task 4.2). Jittered networks shift the timing tail and ~30% of
		// seeds miss the relay deadline — those runs surface via sweep tests
		// that don't assert per-cell Match.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "BFT-comparison.md Table 3 'Validity-divergence 2-2 split: ✗ algebraic limit'. Generalized to N-2f NV ops at any n; at f=1, n=4 this is the canonical 2-2 split (op3, op4 NV).",
}

// ---- Validity divergence (3-1: minority NV, σ-pool reaches anyway) ---

var scenarioValidityDivergence3_1 = Scenario{
	Name:  "ValidityDivergence_3_1",
	Title: "Validity divergence: minority NV (3-1)",
	Group: "Host validity",
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
		// QBFT: 3 ops PREPARE on V (op4 doesn't, host says invalid) → quorum
		// reached → COMMIT-quorum → R1 succeeds.
		"QBFT": ExpectSuccessFastest,
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
		// QBFT: R1 PREPARE pool insufficient (#valid honest = f, < quorum=2f+1)
		// → R1 timeout → R2 fresh V → succeeds.
		"QBFT": ExpectSuccessFallThrough,
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
		// QBFT: R1 PREPARE-quorum unreachable (op-NV+byz-silent); R2 fresh-V
		// host-validates at layer 1 → succeeds.
		"QBFT": ExpectSuccessFallThrough,
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
		// QBFT: R1 PREPARE-quorum unreachable; R2 fresh-V at layer 1 host-validates → succeeds.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "OBFT.md §Failure modes / Validity-divergence deadlock #2. Worst-case all-NV non-leaders plus f byz silent — σ-pool collapses to leader-only, NR-pool capped below qEnc by byz silence.",
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
		// QBFT: R1 PREPARE pool = leader + f byz = 1+f < quorum=2f+1; R1 timeout;
		// R2 fresh-V at layer 1 host-validates → succeeds.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "OBFT.md §Failure modes / Validity-divergence deadlock #3. Byz emits σ on V (cryptographically unattributable — passive σ contribution is honest-equivalent message-wise) but consumes f-budget, so σ-pool still misses qV. The spec's spec-3 algebraic miss configuration.",
}

// ---- σ-refusal (byz never contributes) --------------------------------

var scenarioSigmaRefusal = Scenario{
	Name:  "SigmaRefusal",
	Title: "Byz σ-refusal (silent) within f-bound",
	Group: "Silent operators",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzSigmaRefusal, ByzOperators: []OperatorID{4}}
	},
	Expect: map[string]ExpectClass{
		// OBFT: byz never σ-emits; 3 honest can still reach qV=3 at L_0
		// (leader + 2 non-leader honest). Healthy path holds.
		"OBFT": ExpectSuccessFastest,
		// QBFT: byz never PREPAREs/COMMITs; 3 honest can still reach qV=3
		// at R1 (3 PREPAREs and 3 COMMITs from honest). Healthy path holds.
		"QBFT": ExpectSuccessFastest,
	},
	Note: "Within f-bound, single byz silence doesn't disrupt either protocol's healthy path.",
}

// ---- WithholdLeader at deepest layer (Phase 3) ------------------------

var scenarioWithholdLeaderDeepest = Scenario{
	Name:  "WithholdLeader_Deepest",
	Title: "Deepest-layer leader withholds",
	Group: "Silent operators",
	Apply: func(cfg *SimConfig) {
		// Default rotation: op[k % N] leads layer k. At K=N convention, op{N}
		// leads the deepest layer L_{N-1}. Pick byz=op{N} so the pattern's
		// "silence at the deepest layer they lead" check fires at any cluster
		// size (n=4 → byz=op4 leads L_3; n=7 → byz=op7 leads L_6; etc.).
		cfg.Byz = ByzPattern{Kind: ByzWithholdLeader, ByzOperators: []OperatorID{OperatorID(cfg.N)}}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFastest, // L_0 still healthy; deepest never reached
		"QBFT": ExpectNotApplicable,  // OBFT-specific (layer concept)
	},
	Note: "Class A spec test: deepest-layer leader silenced. L_0 is healthy at any n → cluster decides at L_0 without needing L_{N-1}.",
}

// ---- Cert withholding (Phase 3) ---------------------------------------

var scenarioCertWithholding = Scenario{
	Name:  "CertWithholding",
	Title: "Byz withholds cert gossip",
	Group: "Silent operators",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzCertWithholding, ByzOperators: []OperatorID{4}}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFastest, // honest ops reconstruct independently
		"QBFT": ExpectNotApplicable,  // no chained-cert gossip in QBFT
	},
	Note: "Byz refuses cert gossip; honest ops reconstruct independently → healthy path holds.",
}

// ---- Rule 1 (cross-signing) evidence (Phase 3) ------------------------

var scenarioCrossSigningRule1 = Scenario{
	Name:  "CrossSigning_Rule1",
	Title: "Cross-signing evidence (Rule 1: σ + NR exclusivity)",
	Group: "OBFT-specific attacks",
	Apply: func(cfg *SimConfig) {
		// byz=op2 (L_1 leader by default rotation): silent at L_1 → real NR at L_1;
		// adapter forges σ at L_1 in commit → Rule 1 fires at honest receivers.
		cfg.Byz = ByzPattern{Kind: ByzCrossSigning, ByzOperators: []OperatorID{2}}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFastest, // L_0 leader op1 still broadcasts; healthy
		"QBFT": ExpectNotApplicable,  // OBFT-specific Rule 1 (σ + NR exclusivity)
	},
	Note: "Rule 1 evidence: byz emits σ + NR at same layer. L_0 leader unaffected → healthy decides.",
}

// ---- Rule 5 (fake plaintext σ at L_0) evidence (Phase 3) --------------

var scenarioFakePlaintextSigmaRule5 = Scenario{
	Name:  "FakePlaintextSigma_Rule5",
	Title: "Forged plaintext σ at L_0 (Rule 5)",
	Group: "OBFT-specific attacks",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzFakePlaintextSigma, ByzOperators: []OperatorID{2}}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFastest, // L_0 still healthy via the 3 honest σ partials
		"QBFT": ExpectNotApplicable,  // OBFT-specific Rule 5 (cryptoFake at L_0)
	},
	Note: "Rule 5 evidence: byz emits forged plaintext σ at L_0; honest receivers detect cryptoFake immediately. Cluster decides via 3 honest σ partials at L_0.",
}

// ---- Rule 3 (cross-onion equivocation) evidence (Phase 3) -------------

var scenarioCrossOnionEquivocationRule3 = Scenario{
	Name:  "CrossOnionEquivocation_Rule3",
	Title: "Cross-onion equivocation (Rule 3)",
	Group: "OBFT-specific attacks",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:         ByzCrossOnionEquivocation,
			ByzOperators: []OperatorID{2},
			Layer:        0,
		}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFastest, // 3 honest σ partials at L_0 still reach quorum
		"QBFT": ExpectNotApplicable,  // OBFT-specific Rule 3 (cross-onion σ)
	},
	Note: "Rule 3 evidence: byz emits two structurally-distinct Commits with different σ at L_0. Honest receivers fire top-level + per-layer Rule 3.",
}

// ---- Host flip mid-slot (Phase 4) -------------------------------------

var scenarioHostFlipMidSlot = Scenario{
	Name:  "HostFlipMidSlot",
	Title: "Host valid at L_0/R1, flips invalid deeper",
	Group: "Host validity",
	Apply: func(cfg *SimConfig) {
		cfg.Host = HostFlipMidSlot{ValidUntilLayer: 0}
	},
	Expect: map[string]ExpectClass{
		// OBFT: ops σ-emit at L_0 (host valid); slot decides at L_0.
		// Deeper layers' "invalid" verdict isn't queried (validate-once-and-lock).
		"OBFT": ExpectSuccessFastest,
		// QBFT: round 1 PROPOSE host-validates; cluster decides at R1.
		"QBFT": ExpectSuccessFastest,
	},
	Note: "Host valid only at layer 0 / round 1. Healthy decision at fastest path; 'invalid' at deeper layers/rounds is moot under healthy propagation.",
}

// ---- Late L_0 leader broadcast (Phase 3 — Class A spec) ---------------

var scenarioLateLeaderBroadcast = Scenario{
	Name:  "LateLeaderBroadcast_L0",
	Title: "Late L_0 leader broadcast (past T_commit)",
	Group: "Propagation issues",
	Apply: func(cfg *SimConfig) {
		// byz=op1 is L_0 leader by default rotation; broadcasts past T_commit.
		cfg.Byz = ByzPattern{Kind: ByzLateLeaderBroadcast, ByzOperators: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		// OBFT: L_0 σ-pool insufficient (byz bundle past T_commit, honest reject);
		// NR-quorum at L_0 unlocks L_1 → honest L_1 leader broadcasts on time → fall-through.
		"OBFT": ExpectSuccessFallThrough,
		// QBFT: no layer concept; "late broadcast" doesn't translate cleanly.
		"QBFT": ExpectNotApplicable,
	},
	Note: "Class A spec test (asymmetric propagation past T_commit). Validates per-layer absorption-window mechanism; cluster falls through to deeper layer with wider receiver-side absorption.",
}

// ---- Host invalid until L_1 (Phase 4) ---------------------------------

var scenarioHostInvalidUntilL1 = Scenario{
	Name:  "HostInvalidUntilL1",
	Title: "Host invalid at L_0/R1, valid from L_1/R2",
	Group: "Host validity",
	Apply: func(cfg *SimConfig) {
		cfg.Host = HostInvalidUntilLayer{InvalidUntilLayer: 0}
	},
	Expect: map[string]ExpectClass{
		// OBFT: L_0 host-invalid → all NR at L_0 → NR-quorum unlocks L_1 →
		// L_1 host-valid → σ-emit at L_1 → decides at L_1 (fall-through).
		"OBFT": ExpectSuccessFallThrough,
		// QBFT: round 1 PROPOSE rejected → R1 timeout → R2 fresh-V validates
		// (host valid at R2 = framework round 1) → decides at R2.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "Host invalid at layer 0 / round 1 only. Both protocols recover via fall-through to deeper layer / next round. Exercises round-aware host validation in QBFT.",
}

// ---- partial equivocation (natural recovery; OBFT.md:443) -------------

// Generalized from the f=1, n=4 "2-1 split". Byz delivers V_a to 2f honest
// and V_b to 1 honest; σ-pool on V_a = 2f recipients + leader's σ_L^V(V_a)
// = 2f+1 = qV → quorum reaches at L_0. Pigeonhole 2 limits cluster-wide to
// one V reaching qV: σ-pool on V_b = 1 + leader's σ_L^V(V_b) = 2 < qV (for
// f ≥ 1). Slot succeeds at L_0 with V_a; equivocation still slashable.
var scenarioPartialEquivocationNaturalRecovery = Scenario{
	Name:  "PartialEquivocation_NaturalRecovery",
	Title: "Leader equivocates: 2-1 natural recovery",
	Group: "Leader equivocation",
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// 2f recipients for V_a (op2..op{2f+1}), 1 recipient for V_b
		// (op{2f+2}). Total 2f+1 honest receive bundles; remaining
		// N-2f-2 honest receive nothing. At n=4 f=1 this is op2,op3 → V_a,
		// op4 → V_b (the canonical "2-1 split").
		recipients := make([]OperatorID, 0, 2*f+1)
		for i := 0; i < 2*f; i++ {
			recipients = append(recipients, OperatorID(i+2)) // V_a recipients
		}
		recipients = append(recipients, OperatorID(2*f+2)) // V_b recipient
		cfg.Byz = ByzPattern{
			Kind:         ByzPartialEquivocation,
			ByzOperators: []OperatorID{1},
			Recipients:   recipients,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool on V_a = 2f recipients + leader's σ_L^V(V_a) = 2f+1 = qV.
		// Pigeonhole 2 holds: only V_a reaches qV cluster-wide. Slot succeeds
		// at L_0 with V_a even though leader equivocated. Equivocation evidence
		// still gossipable — success doesn't suppress slashing.
		"OBFT": ExpectSuccessFastest,
		// QBFT: PREPARE-pool on V_a = 2f honest (byz leader runs no real
		// Instance, no PREPARE from leader); pool on V_b = 1. Both < quorum →
		// R1 timeout → R2 honest leader proposes fresh V → succeeds.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "Byz fumbles equivocation timing; one V reaches qV naturally. Validates Pigeonhole 2 'at most one V reaches qV cluster-wide' under nonzero σ-pools on both V's. OBFT.md:443 (case analysis) / OBFT.md:477 (BFT-comparison row 'Byzantine leader equivocates, 2-1 split'). Distinct from EquivocateSigmaLockedSplit (σ-locked split slot-miss at OBFT.md:452).",
}

// ---- Mesh-flakiness deadlock (OBFT.md §Properties summary) -------------

// Spec quote (§Properties / "Mesh-flakiness tolerance"): "A mesh-flaky
// honest operator who fails to observe peer σ-emits within the NR-decision
// window can NR-emit incorrectly, becoming a byzantine-equivalent f-budget
// consumer for that slot. Combined with byz σ-refusal, this creates a
// deadlock that the protocol cannot recover from within the slot."
//
// Setup at f=1 n=4 (generalized via cfg.F()):
//   - op1: honest L_0 leader.
//   - op2..op{f+1}: mesh-flaky honest — 2·BTT inbound delay on all
//     messages via PerReceiverDelay. Phase-1 bundles for L_0 and L_1
//     arrive past T_commit at these ops (FetchAt[k] + 2·BTT > T_commit
//     for k ≤ 1 at the default schedule), so they retain no V and emit
//     NR at L_0. (L_2 and L_3 bundles arrive in time at flaky ops, but
//     L_0/L_1 NR's are enough to short NR-pool below qEnc.)
//   - op{N-f+1}..op{N}: byz σ-refusal (never emits commit / NR).
//
// Cluster σ at L_0:
//   - op1's Phase-1 σ_L^V = 1 partial.
//   - Honest non-leader non-flaky: σ via commit. Count = N-1-2f.
//   - Total σ = N-2f = f+1 < qV=2f+1 (when f ≥ 1).
//
// Cluster NR at L_0:
//   - Flaky ops emit NR = f partials.
//   - Byz silent: 0 contribution.
//   - Total NR = f < qEnc=2f+1.
//
// Both quorums short → MISS at L_0 with no fall-through (chain stays
// sealed). The flaky honest's incorrect NR is byz-equivalent f-budget
// consumption per spec's mesh-flakiness analysis.
var scenarioMeshFlakiness = Scenario{
	Name:  "MeshFlakiness",
	Title: "Mesh-flaky honest + byz σ-refusal",
	Group: "Propagation issues",
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// f mesh-flaky ops at op2..op{f+1}: 2·BTT inbound delay.
		flakyOverrides := make(map[OperatorID]time.Duration, f)
		for i := 0; i < f; i++ {
			flakyOverrides[OperatorID(i+2)] = 2 * cfg.BTT
		}
		cfg.Network = PerReceiverDelay{
			Inner:     ConstantDelay{D: cfg.BTT},
			Overrides: flakyOverrides,
		}
		// f byz σ-refusal at op{N-f+1}..op{N}.
		byzOps := make([]OperatorID, f)
		for i := 0; i < f; i++ {
			byzOps[i] = OperatorID(cfg.N - f + 1 + i)
		}
		cfg.Byz = ByzPattern{Kind: ByzSigmaRefusal, ByzOperators: byzOps}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=f+1<qV=2f+1; NR-pool=f<qEnc=2f+1; both short → miss.
		"OBFT": ExpectMiss,
		// QBFT: flaky receivers see PROPOSE/PREPAREs with delay but non-flaky
		// non-byz honest count (N-1-2f) PREPARE among themselves on time;
		// quorum (qV=2f+1) reaches at R1 once flaky ops' delayed PREPAREs
		// arrive. Decides at R1 across all SSV cluster sizes — the
		// QBFT-vs-OBFT-family asymmetry the spec calls out under mesh
		// flakiness (PREPARE-pool quorum-by-arrival vs OBFT's hard T_commit
		// cutoff with no late retention).
		"QBFT": ExpectSuccessFastest,
	},
	Note: "OBFT.md §Properties / Mesh-flakiness tolerance: flaky honest NR-emits incorrectly + byz σ-refusal → OBFT both quorums short → no fall-through (miss). QBFT recovers at R1 (PREPARE-pool reaches qV once delayed flaky PREPAREs arrive — no hard cutoff). Validates the spec's 'mesh-flaky honest = f-budget consumer' claim and the QBFT-vs-OBFT asymmetry.",
}

// ---- Asymmetric-propagation f-boundary (OBFT.md §Liveness) -----------

// Spec quote (§Liveness / "Adversarial scheduling within partial synchrony"):
// "Liveness — adversary delays V to ≤ 1 honest past T_commit: The other 2
// honest σ-emit on time; σ-pool = 2 + leader = 3 = qV. **Quorum reaches
// without the delayed operator.**"
//
// Pure NETWORK-only — no byz operator. Distinct from scenarioHV1Selective
// (which is byz-leader-driven via deliberate unicast). Tests the network-
// side of the same algebraic boundary the spec describes.
//
// At any n with f = (n-1)/3: marking f non-leader receivers slow (inbound
// delay > B_0) leaves N-f operators able to σ-emit on time → σ-pool =
// leader + (N-1-f) on-time non-leaders = N-f = 2f+1 = qV. Quorum reaches.
//
// At n=4, f=1: 1 slow receiver (op2) at 3·BTT inbound delay. Phase-1
// bundle for L_0 arrives at op2 at FetchAt[0] + 3·BTT = 3150 + 600 = 3750ms
// > T_commit=3400ms → op2 rejects. σ-pool at L_0 = op1(leader) + op3 + op4
// = 3 = qV. Cluster decides at L_0.
var scenarioAsymmetricPropagation_FSlow_Success = Scenario{
	Name:  "AsymmetricPropagation_FSlow_Success",
	Title: "Asymmetric propagation: f slow receivers (success)",
	Group: "Propagation issues",
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// op2..op{f+1}: 3·BTT inbound delay. Pushes Phase-1 bundle arrival
		// past T_commit at those ops; σ-pool retains the remaining
		// (N-1-f) on-time honest non-leaders + leader's σ_L^V = N-f = qV.
		overrides := make(map[OperatorID]time.Duration, f)
		for i := 0; i < f; i++ {
			overrides[OperatorID(i+2)] = 3 * cfg.BTT
		}
		cfg.Network = PerReceiverDelay{
			Inner:     ConstantDelay{D: cfg.BTT},
			Overrides: overrides,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool = N-f = qV at L_0; decides at L_0 with the slow ops
		// NR'd in their local commits but irrelevant to cluster σ-quorum.
		"OBFT": ExpectSuccessFastest,
		// QBFT: slow ops PREPARE late (R1 PROPOSE arrives at them at
		// 300+3·BTT=900ms; their PREPAREs arrive at others by 1100ms);
		// PREPARE-quorum reaches at fast ops within R1 (RT=2s); R1 succeeds.
		"QBFT": ExpectSuccessFastest,
	},
	Note: "OBFT.md §Liveness 'Adversary delays V to ≤ 1 honest past T_commit'. Pure network-driven (no byz). Cluster σ-pool reaches qV at L_0 from the (N-f) in-time operators. Complement to HV1SelectiveDelivery (which is byz-leader-driven at the SAME algebraic boundary).",
}

// Spec quote (§Liveness / "Adversary delays V to ≥ 2 honest past T_commit"):
// At h_V=1 shape (1 honest receives V, 2 honest delayed), recipient is
// σ-locked and can't NR; NR-pool = 2 < qEnc → **chain stays sealed at this
// layer with no fall-through. ✗ slot-miss cleanly.**
//
// At any n with f+1 slow non-leaders: σ-pool = leader + (N-1-(f+1)) =
// N-f-1 < qV (since qV = 2f+1 and N-f-1 = 2f at N=3f+1). NR-pool = f+1
// (slow honest NR) < qEnc = 2f+1 when f ≥ 1. Both quorums short → MISS at
// L_0 with no fall-through (chain at L_0 stays sealed).
//
// At n=4, f=1: 2 slow receivers (op2, op3). σ-pool = op1+op4 = 2 < qV.
// NR-pool = op2+op3 = 2 < qEnc. Miss.
//
// Pure NETWORK-only; bracketed against scenarioAsymmetricPropagation_FSlow_Success
// to make the boundary observable directly.
var scenarioAsymmetricPropagation_FPlus1Slow_Miss = Scenario{
	Name:  "AsymmetricPropagation_FPlus1Slow_Miss",
	Title: "Asymmetric propagation: f+1 slow receivers (miss)",
	Group: "Propagation issues",
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// op2..op{f+2}: 3·BTT inbound delay. (f+1) honest slow.
		overrides := make(map[OperatorID]time.Duration, f+1)
		for i := 0; i < f+1; i++ {
			overrides[OperatorID(i+2)] = 3 * cfg.BTT
		}
		cfg.Network = PerReceiverDelay{
			Inner:     ConstantDelay{D: cfg.BTT},
			Overrides: overrides,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=N-f-1<qV; NR-pool=f+1<qEnc; both short; miss at L_0
		// with no fall-through (chain stays sealed at L_0). Class A
		// asymmetric-propagation-past-T_commit per §Failure modes.
		"OBFT": ExpectMiss,
		// QBFT: R1 PREPARE pool eventually reaches qV once slow ops'
		// late PREPAREs arrive within R1's window (RT=2s). Succeeds at R1.
		// The QBFT-vs-OBFT asymmetry on this exact spec configuration.
		"QBFT": ExpectSuccessFastest,
	},
	Note: "OBFT.md §Liveness / §Failure modes — h_V=1-shape asymmetric propagation. (f+1) honest miss V at T_commit; σ-pool < qV and NR-pool < qEnc; chain stays sealed; OBFT misses. QBFT R1 PREPARE eventually reaches qV from late-arriving slow ops within RT, so R1 succeeds. Pure network-driven (no byz) — distinct from scenarioHV1SelectiveDelivery which is byz-engineered.",
}

// ---- All-layers-silent cascade miss (deepest-layer load-bearing) -----

// Spec referent: OBFT.md §Failure modes / "Late deepest-layer leader
// broadcast" and the Class A "all-layer cascade failure" case. The
// existing scenarioMultiSilent (K=3) tests fall-through to L_3 when
// only the top 3 layers are silent; this scenario complements it by
// silencing ALL K layers — exercising the path where OBFT walks every
// layer via NR-quorum but finds no σ at any of them, then misses.
//
// At K = N (default), silencing all K leaders is "every cluster member
// silent at their leader role" — operationally equivalent to all leaders
// suffering coincident silent/late-broadcast failures (Class A: cluster's
// implicit assumption that ≥ 1 leader broadcasts on time is violated).
// Rare in practice; the test is the boundary check that confirms
// graceful miss when the boundary breaks.
//
// Outcome at OBFT (K=N=4, f=1, byz=0):
//   - L_0: all 3 honest non-leaders + leader silent → no V retained
//     anywhere → all 4 emit NR at L_0. NR-pool = 4 = qEnc ✓.
//   - Chain unlocks to L_1: same shape, NR-pool = qEnc → unlocks L_2.
//   - L_2 → L_3 via same NR-quorum. L_3 has no NR tag (deepest).
//   - No σ at any layer → no qV reached → MISS cleanly. Safety holds.
//
// Outcome at QBFT (K silent rounds within RT budget):
//   - R1 PROPOSE never arrives → R1 timeout (~2s).
//   - R2 PROPOSE never arrives → R2 timeout (~4s, past RelayCutoff).
//   - MISS by deadline.
var scenarioMultiSilent_AllLayers = Scenario{
	Name:  "MultiSilent_AllLayers",
	Title: "All K leaders silent (cascade miss)",
	Group: "Silent operators",
	Apply: func(cfg *SimConfig) {
		k := cfg.K
		if k == 0 {
			k = DefaultK(cfg.N)
		}
		// All K layers silent: K=cluster.K means OnlyHonestLayer=K, so the
		// "layer < OnlyHonestLayer" check in byzMultiSilent.LeaderBroadcastPlan
		// fires for every layer in [0, K), silencing every leader.
		cfg.Byz = ByzPattern{Kind: ByzMultiSilent, K: k}
	},
	Expect: map[string]ExpectClass{
		// OBFT: walks all K layers via NR-quorum; deepest layer has no σ;
		// no NR tag past deepest → MISS cleanly. Safety holds.
		"OBFT": ExpectMiss,
		// QBFT: K silent rounds consume the RT budget before any round
		// can decide. R1 + R2 timeouts > 4s RelayCutoff → MISS.
		"QBFT": ExpectMiss,
	},
	Note: "OBFT.md §Failure modes / Backup-leader cascade failure + Late deepest-layer leader broadcast. Complement to scenarioMultiSilent (K-1 silent → success at L_{K-1}): silencing ALL K layers exercises the deepest-layer miss path where OBFT walks every layer via NR-quorum and finds nothing. Both protocols miss cleanly; safety invariants hold.",
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
		// QBFT: R1 PREPARE pool short (host-NV ops don't PREPARE; byz silent);
		// R2 fresh-V at layer 1 host-validates → succeeds. (HostInvalidForOperators
		// is layer-0-scoped; round 2 = layer 1 sees all-valid.)
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "OBFT.md §Failure modes — Validity-divergence deadlock, leader-NV symmetric variant. Validates the spec's σ_V-lock-despite-host-NV invariant: a Phase-1-σ-emitted leader stays σ-locked even when their host returns NV. Same algebraic miss outcome as the non-leader-NV passive-byz scenarios; the leader-NV-locked configuration is the additional spec coverage.",
}
