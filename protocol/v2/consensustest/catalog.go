package consensustest

// Catalog is the canonical list of cross-protocol scenarios. Each Scenario
// declares a SimConfig modifier and per-protocol expectations sourced from
// docs/BFT-comparison.md and docs/OBFT.md §Application.
//
// Scenarios are ordered roughly by failure-mode taxonomy: healthy → silent
// leader → multi-silent → equivocation patterns → validity divergence →
// OBFT-specific patterns (n/a for QBFT).
//
// Add new scenarios at the end to keep order stable for matrix-rendering.
var Catalog = []Scenario{
	scenarioHealthy,
	scenarioSilentLeaderL0,
	scenarioMultiSilent,
	scenarioEquivocate111,
	scenarioEquivocateAllNR,
	scenarioEquivocateSigmaLockedSplit,
	scenarioHV1SelectiveDelivery,
	scenarioFakeEncryptedPresence,
	scenarioValidityDivergence_2_2,
	scenarioSigmaRefusal,
}

// ---- Healthy ------------------------------------------------------------

var scenarioHealthy = Scenario{
	Name: "Healthy",
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
	Name: "PrimaryLeaderSilent",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzSilentLeader, PrimaryByz: 1}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFallThrough, // V_0 silent → in-round to V_1
		"QBFT": ExpectSuccessFallThrough, // R1 silent → R2 success
	},
	Note: "Primary leader silent. OBFT falls through K-layer in-round; QBFT round-changes to R2 (pays RT timeout).",
}

// ---- Multi-silent (top 3 of 4 leaders silent) --------------------------

var scenarioMultiSilent = Scenario{
	Name: "MultiSilent_K3",
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

// ---- Leader equivocates 1-1-1 ------------------------------------------

var scenarioEquivocate111 = Scenario{
	Name: "Equivocate_111",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzEquivocate111, PrimaryByz: 1}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pools split below qV; no NR-quorum; slot misses.
		"OBFT": ExpectMiss,
		// QBFT: PREPARE pool fragments; R1 timeout → R2 with fresh V → success.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "BFT-comparison.md Table 3: QBFT recovers via R2 fresh-V; OBFT misses (R-invariant).",
}

// ---- Leader equivocates all-NR (floods both V's to all honest) --------

var scenarioEquivocateAllNR = Scenario{
	Name: "Equivocate_AllNR",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzEquivocateAllNR, PrimaryByz: 1}
	},
	Expect: map[string]ExpectClass{
		// OBFT: every honest retains ≥ 2 V's, NRs per equivocation rule;
		// NR-quorum at L_0 → fall-through to L_1.
		"OBFT": ExpectSuccessFallThrough,
		// QBFT: byz proposer broadcasts conflicting PROPOSEs; honest detect
		// equivocation; R1 PREPARE pool insufficient on any V; R1 timeout →
		// R2 with fresh V → success.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "Both recover at fall-through cost; OBFT in-round (cheap), QBFT pays RT (expensive).",
}

// ---- σ-locked split (1-1) ---------------------------------------------

var scenarioEquivocateSigmaLockedSplit = Scenario{
	Name: "Equivocate_SigmaLockedSplit",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:       ByzEquivocateSigmaLockedSplit,
			PrimaryByz: 1,
			Recipients: []OperatorID{2, 3}, // index 0 → V_a, index 1 → V_b
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool on each V at 2 < qV=3; NR-pool can't reach (σ-locked
		// honest can't NR); slot misses.
		"OBFT": ExpectMiss,
		// QBFT: PREPARE pool splits; R1 timeout → R2 fresh V → success.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "OBFT-family R-invariant slot-miss; QBFT R2 recovers.",
}

// ---- h_V=1 selective delivery (OBFT-specific) -------------------------

var scenarioHV1SelectiveDelivery = Scenario{
	Name: "HV1SelectiveDelivery",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:       ByzHV1SelectiveDelivery,
			PrimaryByz: 1,
			Recipients: []OperatorID{2}, // exactly one honest receives V
		}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectMiss,
		"QBFT": ExpectNotApplicable,
	},
	Note: "OBFT-specific deadlock pattern. QBFT's round-change recovers structurally; pattern doesn't translate.",
}

// ---- Fake encrypted presence (OBFT-specific Rule 4) -------------------

var scenarioFakeEncryptedPresence = Scenario{
	Name: "FakeEncryptedPresence",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:       ByzFakeEncryptedPresence,
			PrimaryByz: 1,
			Layer:      1,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: byz silent at L_0 → NR-quorum unlocks L_1 decryption →
		// honest L_1 leader's bundle reconstructs at L_1; byz's L_1
		// garbage triggers Rule 4 evidence (recorded but not asserted here
		// — adapter tracks via PerOp.EvidenceCount).
		"OBFT": ExpectSuccessFallThrough,
		"QBFT": ExpectNotApplicable,
	},
	Note: "OBFT-specific (no chained encryption in QBFT). Verifies Rule 4 detection path under real-BLS or stub-IBE.",
}

// ---- Validity divergence (2-2 split at L_0) ---------------------------

var scenarioValidityDivergence_2_2 = Scenario{
	Name: "ValidityDivergence_2_2",
	Apply: func(cfg *SimConfig) {
		cfg.Host = HostInvalidForOperators{
			Layer:     0,
			Operators: map[OperatorID]bool{3: true, 4: true},
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=2 (leader+1) < qV; NR-pool=2 (NV) < qEnc; chained
		// decryption blocked → slot misses (algebraic limit per Table 3).
		"OBFT": ExpectMiss,
		// QBFT: depends on round-2 fresh V landing on a stable head;
		// outcome depends on the host's behavior across rounds. Acceptable
		// as success or miss.
		"QBFT": ExpectSuccessOrMiss,
	},
	Note: "BFT-comparison.md Table 3 'Validity-divergence 2-2 split: ✗ algebraic limit' for both OBFT-family; QBFT depends on whether round-change refetches at moved head.",
}

// ---- σ-refusal (byz never contributes) --------------------------------

var scenarioSigmaRefusal = Scenario{
	Name: "SigmaRefusal",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzSigmaRefusal, PrimaryByz: 4}
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
