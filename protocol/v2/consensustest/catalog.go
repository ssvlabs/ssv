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
	scenarioValidityDivergence2_2,
	scenarioSigmaRefusal,
	scenarioWithholdLeaderDeepest,
	scenarioCertWithholding,
	scenarioCrossSigningRule1,
	scenarioFakePlaintextSigmaRule5,
	scenarioCrossOnionEquivocationRule3,
	scenarioHostFlipMidSlot,
	scenarioHostInvalidUntilL1,
	scenarioLateLeaderBroadcast,
	scenarioPartialEquivocation2_1,
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
		cfg.Byz = ByzPattern{Kind: ByzEquivocate111, ByzOperators: []OperatorID{1}}
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

// ---- σ-locked split (1-1) ---------------------------------------------

var scenarioEquivocateSigmaLockedSplit = Scenario{
	Name: "Equivocate_SigmaLockedSplit",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:         ByzEquivocateSigmaLockedSplit,
			ByzOperators: []OperatorID{1},
			Recipients:   []OperatorID{2, 3}, // index 0 → V_a, index 1 → V_b
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
			Kind:         ByzHV1SelectiveDelivery,
			ByzOperators: []OperatorID{1},
			Recipients:   []OperatorID{2}, // exactly one honest receives V
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

// ---- Validity divergence (2-2 split at L_0) ---------------------------

var scenarioValidityDivergence2_2 = Scenario{
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
	Name: "WithholdLeader_Deepest",
	Apply: func(cfg *SimConfig) {
		// Default rotation: L_0=op1, L_1=op2, L_2=op3, L_3=op4. byz=op4 silences L_3.
		cfg.Byz = ByzPattern{Kind: ByzWithholdLeader, ByzOperators: []OperatorID{4}}
	},
	Expect: map[string]ExpectClass{
		"OBFT": ExpectSuccessFastest, // L_0 still healthy; deepest never reached
		"QBFT": ExpectNotApplicable,  // OBFT-specific (layer concept)
	},
	Note: "Class A spec test: deepest-layer leader silenced. At K=N=4, L_0 healthy → cluster decides without needing L_3.",
}

// ---- Cert withholding (Phase 3) ---------------------------------------

var scenarioCertWithholding = Scenario{
	Name: "CertWithholding",
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
	Name: "CrossSigning_Rule1",
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
	Name: "FakePlaintextSigma_Rule5",
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
	Name: "CrossOnionEquivocation_Rule3",
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
	Name: "HostFlipMidSlot",
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
	Name: "LateLeaderBroadcast_L0",
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
	Name: "HostInvalidUntilL1",
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

// ---- 2-1 partial equivocation (natural recovery; OBFT.md:443) ---------

// Natural-recovery analysis is calibrated for f=1, n=4 specifically: σ-pool
// on V_a = 2 honest + leader's σ_L^V = 3 = qV. At larger n (qV = 2f+1 grows
// faster than the 2 fixed V_a recipients), this pattern degrades to a
// 2-1-silent-rest split that misses qV at L_0 — falling through via NR
// instead. Scenario currently runs only at the matrix's default n=4 base.
var scenarioPartialEquivocation2_1 = Scenario{
	Name: "PartialEquivocation_2_1",
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:         ByzPartialEquivocation,
			ByzOperators: []OperatorID{1},
			Recipients:   []OperatorID{2, 3, 4}, // V_a → op2, op3; V_b → op4
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool on V_a = op2 + op3 + leader's σ_L^V(V_a) = 3 = qV at f=1, n=4.
		// Pigeonhole 2 holds: only V_a reaches qV cluster-wide. Slot succeeds at L_0
		// with V_a even though leader equivocated. Equivocation evidence still
		// gossipable — success doesn't suppress slashing.
		"OBFT": ExpectSuccessFastest,
		// QBFT: PREPARE-pool on V_a = 2 honest (byz leader runs no real Instance,
		// no PREPARE from leader); pool on V_b = 1. Both < quorum → R1 timeout →
		// R2 honest leader proposes fresh V → succeeds.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "Byz fumbles equivocation timing; one V reaches qV naturally. Validates Pigeonhole 2 'at most one V reaches qV cluster-wide' under nonzero σ-pools on both V's. OBFT.md §Liveness equivocation case-analysis line 443; row 'Byzantine leader equivocates, 2-1 split' in OBFT.md liveness-comparison table line 477. Distinct from EquivocateSigmaLockedSplit (1-1-NR slot-miss at OBFT.md:452).",
}
