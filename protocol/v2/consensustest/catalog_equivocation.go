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
	Modes: []Mode{ModeCorrectness, ModeStress},
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
		// QBFT: PREPARE pool splits; R1 timeout → R2 fresh V → success.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "OBFT-family R-invariant slot-miss at any cluster size (generalized 1-1 → f-f); QBFT R2 recovers.",
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
		// QBFT: PREPARE-pool on V_a = 2f honest (byz leader runs no real
		// Instance, no PREPARE from leader); pool on V_b = 1. Both < quorum →
		// R1 timeout → R2 honest leader proposes fresh V → succeeds.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "Byz fumbles equivocation timing; one V reaches qV naturally. Validates Pigeonhole 2 'at most one V reaches qV cluster-wide' under nonzero σ-pools on both V's. OBFT.md:443 (case analysis) / OBFT.md:477 (BFT-comparison row 'Byzantine leader equivocates, 2-1 split'). Distinct from EquivocateSigmaLockedSplit (σ-locked split slot-miss at OBFT.md:452).",
}
