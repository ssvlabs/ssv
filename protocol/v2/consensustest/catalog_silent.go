package consensustest

// ---- Silent primary leader ---------------------------------------------

var scenarioSilentLeaderL0 = Scenario{
	Name:  "PrimaryLeaderSilent",
	Title: "Primary leader silent",
	Group: "Silent operators",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzSilentLeader, ByzOperators: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFallThrough, // V_0 silent → in-round to V_1
		"2abOBFT": ExpectSuccessFallThrough, // same recovery shape as OBFT
		"QBFT":    ExpectSuccessFallThrough, // R1 silent → R2 success
	},
	Note: "Primary leader silent. OBFT falls through K-layer in-round; QBFT round-changes to R2 (pays RT timeout).",
}

// ---- Multi-silent (top 3 of 4 leaders silent) --------------------------

var scenarioMultiSilent = Scenario{
	Name:  "MultiSilent_K3",
	Title: "Top K-1 leaders silent (deepest is honest)",
	Group: "Silent operators",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		// Top 3 leaders silent; only the deepest is honest.
		cfg.Byz = ByzPattern{Kind: ByzMultiSilent, K: 3}
	},
	Expect: map[string]ExpectClass{
		// OBFT recovers in-round via K-layer fall-through to L_3.
		"OBFT": ExpectSuccessFallThrough,
		// 2abOBFT: same NR-fall-through path; deepest layer L_3 honest leader
		// reaches σ-quorum on its V via Phase-2a verdict convergence.
		"2abOBFT": ExpectSuccessFallThrough,
		// QBFT needs 3 round-changes (R1, R2, R3 all silent → R4 honest).
		// At RT=2s × 3 timeouts = 6s, exceeds RelayCutoff=4s → MISS.
		"QBFT": ExpectMiss,
	},
	Note: "Top 3 of 4 leaders silent; only the deepest is honest. Structural OBFT-family advantage at any (BFT_start, D) where the healthy path fits — multi-leader-silent fall-through is in-round, vs QBFT's serial round-change exceeding budget.",
}

// ---- σ-refusal (byz never contributes) --------------------------------

var scenarioSigmaRefusal = Scenario{
	Name:  "SigmaRefusal",
	Title: "Byz σ-refusal (silent) within f-bound",
	Group: "Silent operators",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzSigmaRefusal, ByzOperators: []OperatorID{4}}
	},
	Expect: map[string]ExpectClass{
		// OBFT: byz never σ-emits; 3 honest can still reach qV=3 at L_0
		// (leader + 2 non-leader honest). Healthy path holds.
		"OBFT": ExpectSuccessFastest,
		// 2abOBFT: byz never emits Onion2b; 3 honest σ-emit at L_0 (qV met).
		"2abOBFT": ExpectSuccessFastest,
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
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		// Default rotation: op[k % N] leads layer k. At K=N convention, op{N}
		// leads the deepest layer L_{N-1}. Pick byz=op{N} so the pattern's
		// "silence at the deepest layer they lead" check fires at any cluster
		// size (n=4 → byz=op4 leads L_3; n=7 → byz=op7 leads L_6; etc.).
		cfg.Byz = ByzPattern{Kind: ByzWithholdLeader, ByzOperators: []OperatorID{OperatorID(cfg.N)}}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest, // L_0 still healthy; deepest never reached
		"2abOBFT": ExpectSuccessFastest, // same — L_0 succeeds regardless of deepest
		"QBFT":    ExpectNotApplicable,  // OBFT-specific (layer concept)
	},
	Note: "Class A spec test: deepest-layer leader silenced. L_0 is healthy at any n → cluster decides at L_0 without needing L_{N-1}.",
}

// ---- Cert withholding (Phase 3) ---------------------------------------

var scenarioCertWithholding = Scenario{
	Name:  "CertWithholding",
	Title: "Byz withholds cert gossip",
	Group: "Silent operators",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzCertWithholding, ByzOperators: []OperatorID{4}}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest, // honest ops reconstruct independently
		"2abOBFT": ExpectSuccessFastest, // same — honest ops reconstruct independently
		"QBFT":    ExpectNotApplicable,  // no chained-cert gossip in QBFT
	},
	Note: "Byz refuses cert gossip; honest ops reconstruct independently → healthy path holds.",
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
	Modes: []Mode{ModeCorrectness, ModeStress},
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
		// 2abOBFT: same — all-NR at every layer → walk exhausts cleanly.
		"2abOBFT": ExpectMiss,
		// QBFT: K silent rounds consume the RT budget before any round
		// can decide. R1 + R2 timeouts > 4s RelayCutoff → MISS.
		"QBFT": ExpectMiss,
	},
	Note: "OBFT.md §Failure modes / Backup-leader cascade failure + Late deepest-layer leader broadcast. Complement to scenarioMultiSilent (K-1 silent → success at L_{K-1}): silencing ALL K layers exercises the deepest-layer miss path where OBFT walks every layer via NR-quorum and finds nothing. Both protocols miss cleanly; safety invariants hold.",
}
