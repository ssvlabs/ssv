package consensustest

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
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzEquivocate111, ByzOperators: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pools split below qV; no NR-quorum; slot misses.
		"OBFT": ExpectMiss,
		// 2abOBFT (post Op5+Op11): each honest retains one of the N-1
		// distinct V's via Phase-1 bundle direct delivery and emits
		// KindValue σ-locked on their own V (with σ partial). When
		// honest ops cross-broadcast, each harvests the alternate V via
		// Op11 from peers' KindValues. Retention grows to 2 → Rule 2
		// fires. BUT post Op5 the equivocation trigger only fires for
		// ops still in EKM coordination state — ops already σ-locked
		// (which is everyone here) cannot pivot to NR. σ-pool[V_i]
		// fragments (each V_i has 1 partial cluster-wide) — none reaches
		// qV. NR-pool also empty (no ops emitted NR). Cluster MISSes at
		// L_0 with no fall-through. **Op5 trade-off**: equivocation
		// post-σ-commit has no recovery path (plan §Op5 line 1253);
		// pre-Op5 this scenario would have fall-through-recovered via
		// A4 NR-pivot. Pigeonhole 2 still guarantees safety (no V
		// reaches qV → no cluster decision on any V).
		"2abOBFT": ExpectMiss,
		// QBFT: PREPARE pool fragments; R1 timeout → R2 with fresh V → success.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no leader to equivocate
	},
	Note: "BFT-comparison.md Table 3: QBFT recovers via R2 fresh-V; OBFT misses (R-invariant). 2ab recovers via NR-fall-through (verdict-pool fragmentation drives row 5 NR-quorum). Pattern emits N-1 distinct V's at any cluster size; the '1-1-1' name reflects f=1.",
}

// ---- Leader equivocates all-NR (floods both V's to all honest) --------

var scenarioEquivocateAllNR = Scenario{
	Name:  "Equivocate_AllNR",
	Title: "Leader equivocates: floods both values to all",
	Group: "Leader equivocation",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzEquivocateAllNR, ByzOperators: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		// OBFT: every honest retains ≥ 2 V's, NRs per equivocation rule;
		// NR-quorum at L_0 → fall-through to L_1.
		"OBFT": ExpectSuccessFallThrough,
		// 2abOBFT: every honest sees both V's → equivocation observed →
		// row 1 NR per receiver → NR-quorum at L_0 → fall-through to L_1.
		"2abOBFT": ExpectSuccessFallThrough,
		// QBFT: byz proposer splits PROPOSE delivery 50/50 (V_a to half,
		// V_b to half); receivers register only the first PROPOSE for the
		// (signer, round) pair (AddFirstMsgForSignerAndRound dedups
		// silently — no equivocation detection here, unlike OBFT's witness
		// re-hydration). PREPARE pool fragments across V_a/V_b; R1 timeout
		// → R2 with fresh V → success.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no leader to equivocate
	},
	Note: "Both recover at fall-through cost; OBFT in-round (cheap, equivocation-NR-driven), QBFT pays RT (expensive, PREPARE-pool-fragmentation-driven). 2abOBFT Expect (SuccessFallThrough) holds under this profile's ConstantDelay (both V's arrive together → all NRDirect → L_1). Under JITTERY delivery, Op6 async-fire shifts 2abOBFT to mostly L_0-fast decide + a ~23% miss tail (safety-preserving) — see docs/2abOBFT-REDESIGN-PLAN.md §Op6 B1 + TestAdapter_Equivocate_AllNR_JitterTradeoff.",
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
	Modes: []Mode{ModeCorrectness, ModeStress},
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
		// 2abOBFT (post Op5+Op11): the 1-hop cascade lets silent ops
		// (op{2f+2..n}) harvest V_a from V_a-recipients' KindValues +
		// A1 upgrade with their own σ partials. σ-pool[V_a] grows past
		// qV cluster-wide via the silent ops' upgrades, even though the
		// V_a recipients themselves σ-lock on V_a and the V_b recipient
		// σ-locks on V_b (no NR pivot possible post Op5). Slot decides
		// at L_0 with V_a. Pigeonhole 2 holds: only V_a reaches qV
		// (V_b has 1 partial cluster-wide). **Recovery via Op5+Op11
		// combo**: the wider Op11 harvest path + Op5's 1-hop cascade
		// converts what was MISS pre-Op5 into a SuccessFastest.
		"2abOBFT": ExpectSuccessFastest,
		// QBFT: PREPARE pool splits; R1 timeout → R2 fresh V → success.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no leader to equivocate
	},
	Note: "OBFT-family R-invariant slot-miss at any cluster size (generalized 1-1 → f-f); QBFT R2 recovers. 2ab recovers in-round via NR-quorum at L_0 (Phase-2a verdict pool fragmentation + row 5).",
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
	Modes: []Mode{ModeCorrectness, ModeStress},
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
		// 2abOBFT (post Op3): the L_0 leader's L0Witness in the Phase-1
		// bundle (BLS partial on V_a from the byz leader) seeds σ-pool[V_a]
		// at every recipient. 2 V_a recipients + leader's L0Witness =
		// σ-pool[V_a] = 3 = qV. Slot succeeds at L_0 on V_a, matching OBFT's
		// natural-recovery behavior. The byz leader equivocated cluster-
		// wide (two distinct Phase-1 bundles V_a / V_b from the same leader);
		// Rule 2 (leader equivocation) evidence is detectable cluster-wide
		// via ObservePhase1Bundle once reflood surfaces both bundles to any
		// single op. Pre-Op3 v4 missed this scenario because KindValue
		// carried no σ-direction-partial and the leader's σ at Phase 2b
		// never reached cluster-wide qV due to fragmentation.
		"2abOBFT": ExpectSuccessFastest,
		// QBFT: PREPARE-pool on V_a = 2f honest (byz leader runs no real
		// Instance, no PREPARE from leader); pool on V_b = 1. Both < quorum →
		// R1 timeout → R2 honest leader proposes fresh V → succeeds.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no leader to equivocate
	},
	Note: "Byz fumbles equivocation timing; one V reaches qV naturally. Validates Pigeonhole 2 'at most one V reaches qV cluster-wide' under nonzero σ-pools on both V's. OBFT.md:443 (case analysis) / OBFT.md:477 (BFT-comparison row 'Byzantine leader equivocates, 2-1 split'). Distinct from EquivocateSigmaLockedSplit (σ-locked split slot-miss at OBFT.md:452). 2ab loses OBFT's σ_V head-start (Variant C) so this fall-throughs to L_1 instead of L_0.",
}
