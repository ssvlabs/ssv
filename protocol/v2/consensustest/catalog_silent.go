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
		"PSigs":   ExpectNotApplicable,      // PSigs has no leader to silence
	},
	Note: "Primary leader silent. OBFT falls through K-layer in-round; QBFT round-changes to R2 (pays RT timeout).",
}

// ---- Multi-silent (top K-1 leaders silent, deepest honest) -------------

var scenarioMultiSilent = Scenario{
	Name:  "MultiSilent_KMinus1",
	Title: "Top K-1 leaders silent (deepest is honest)",
	Group: "Silent operators",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		// Silence top K-1 leaders; only the deepest (L_{K-1}) is honest.
		// K-generic so the scenario probes the "fall-through to deepest"
		// shape consistently across K ∈ {2, 3, 4, ...} sweep points.
		k := cfg.K
		if k == 0 {
			k = DefaultK(cfg.N)
		}
		cfg.Byz = ByzPattern{Kind: ByzMultiSilent, K: k - 1}
	},
	Expect: map[string]ExpectClass{
		// OBFT/2abOBFT: structurally the fall-through is in-round, but the
		// Phase-3 walk cost (ε_3 × K) plus HeaderSubmitHeadroom exceeds the
		// reserved post-RoundEndOffset budget at K ≥ 3 / default ε_3=50ms.
		// At K=2 the walk fits (1×ε_3 = 50ms → decision at 3900ms = deadline
		// → just succeeds); at K ≥ 3 the decision lands past
		// RelayCutoff − HeaderSubmitHeadroom and the slot misses.
		// ExpectSuccessOrMiss reflects the K-dependent boundary.
		"OBFT":    ExpectSuccessOrMiss,
		"2abOBFT": ExpectSuccessOrMiss,
		// QBFT needs K-1 round-changes (R1..R_{K-1} silent → R_K honest).
		// Timing depends on K and RT vs RelayCutoff:
		//   K=2 → 1 timeout → ~2s < 4s → SUCCESS.
		//   K=3 → 2 timeouts → ~4s ≈ cutoff → SUCCESS or MISS (borderline).
		//   K=4 → 3 timeouts → ~6s > 4s → MISS.
		// ExpectSuccessOrMiss accepts the timing-dependent outcome across
		// the sweep's K values.
		"QBFT":  ExpectSuccessOrMiss,
		"PSigs": ExpectNotApplicable, // PSigs has no concept of K-leader silence
	},
	Note: "Top K-1 of K leaders silent; only the deepest is honest. OBFT's K-layer in-round fall-through is structurally faster than QBFT's serial round-changes, but the per-layer Phase-3 walk cost (ε_3) accumulates: at ε_3=50ms / RelayCutoff=4s the deepest-honest case fits within deadline only at K=2; at K ≥ 3 the decision lands past RelayCutoff − HeaderSubmitHeadroom and the slot misses. ExpectSuccessOrMiss for all three protocols reflects the K-dependent boundary.",
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
		// PSigs: same intuition — 3 honest signers reach qV=3 partial-sigs
		// (own + 2 peers each) at 1·BTT from slot start. Within f-bound,
		// byz refusal is transparent to the cluster's collection threshold.
		"PSigs": ExpectSuccessFastest,
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
		// Default rotation: layer k → operators[k % N], i.e. OperatorID
		// (k % N + 1). The deepest layer's leader is therefore
		// OperatorID((K-1) % N + 1). K-generic so the scenario probes the
		// same shape regardless of K vs N (at K=N the deepest leader is
		// op{N}; at K<N it's an earlier op in the rotation).
		k := cfg.K
		if k == 0 {
			k = DefaultK(cfg.N)
		}
		deepestLeader := OperatorID((k-1)%cfg.N + 1)
		cfg.Byz = ByzPattern{Kind: ByzWithholdLeader, ByzOperators: []OperatorID{deepestLeader}}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest, // L_0 still healthy; deepest never reached
		"2abOBFT": ExpectSuccessFastest, // same — L_0 succeeds regardless of deepest
		// QBFT: byz only leads its own round (under round-robin from op1).
		// R1 honest → byz round is never reached → fastest.
		"QBFT":  ExpectSuccessFastest,
		"PSigs": ExpectNotApplicable, // PSigs has no leader-per-layer concept
	},
	Note: "Class A spec test: deepest-layer leader silenced. L_0 / R1 are healthy at any (n, K) → cluster decides at the first leader without needing the silent one.",
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
		"PSigs":   ExpectNotApplicable,  // no cert-gossip path in PSigs either
	},
	Note: "Byz refuses cert gossip; honest ops reconstruct independently → healthy path holds.",
}

// ---- All-layers-silent cascade miss (deepest-layer load-bearing) -----

// Spec referent: OBFT.md §Failure modes / "Late deepest-layer leader
// broadcast" and the Class A "all-layer cascade failure" case. The
// existing scenarioMultiSilent (K-1 silent, deepest honest) tests
// fall-through to L_{K-1}; this scenario complements it by silencing
// ALL K layers — exercising the path where OBFT walks every layer via
// NR-quorum but finds no σ at any of them, then misses.
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
	// Stress-tier gate: applicable only when the sweep's K equals N.
	// Silencing all K leaders is only meaningful when every operator-
	// leader is included — at K<N some operators never lead, and QBFT
	// can round-change to a non-leader operator that succeeds, defeating
	// the "all silent" intent. Apply BELOW forces K=N internally so the
	// scenario stays meaningful in any caller path, but the stress sweep
	// builds one cell per Fields-tuple; without this gate the cell would
	// be recorded under the sweep's (smaller) Fields["K"] while the sim
	// ran at K=N, mis-attributing the data. Skipping at K<N renders the
	// cell as n/a — truthful.
	//
	// runner.RunScenarioOnProtocol (correctness tier) does NOT consult
	// Applies — it always runs Apply, which forces K=N. That preserves
	// the scenario's original correctness coverage (one K=N run) while
	// fixing the stress-side Fields["K"] mis-attribution.
	Applies: func(cfg SimConfig) bool { return cfg.K == cfg.N },
	Apply: func(cfg *SimConfig) {
		// Force K=N so "all K layers silent" silences every operator-leader.
		// At the K=f+1 BFT-min default, only f+1 operators are leaders and
		// QBFT can round-change to a non-leader operator that succeeds —
		// defeating the "all silent" intent of this scenario. Used in the
		// correctness path where cfg.K may be the BaseConfig zero (Validate
		// would default to DefaultK(N), which is less than N). Stress callers
		// have already passed the Applies gate at this point, so K==N here
		// is also guaranteed (the assignment is a no-op for them).
		cfg.K = cfg.N
		// All K layers silent: K=cluster.K means OnlyHonestLayer=K, so the
		// "layer < OnlyHonestLayer" check in byzMultiSilent.LeaderBroadcastPlan
		// fires for every layer in [0, K), silencing every leader.
		cfg.Byz = ByzPattern{Kind: ByzMultiSilent, K: cfg.K}
	},
	Expect: map[string]ExpectClass{
		// OBFT: walks all K layers via NR-quorum; deepest layer has no σ;
		// no NR tag past deepest → MISS cleanly. Safety holds.
		"OBFT": ExpectMiss,
		// 2abOBFT: same — all-NR at every layer → walk exhausts cleanly.
		"2abOBFT": ExpectMiss,
		// QBFT: K silent rounds consume the RT budget before any round
		// can decide. R1 + R2 timeouts > 4s RelayCutoff → MISS.
		"QBFT":  ExpectMiss,
		"PSigs": ExpectNotApplicable, // PSigs has no concept of per-layer leader silence
	},
	Note: "OBFT.md §Failure modes / Backup-leader cascade failure + Late deepest-layer leader broadcast. Complement to scenarioMultiSilent (K-1 silent → success at L_{K-1}): silencing ALL K layers exercises the deepest-layer miss path where OBFT walks every layer via NR-quorum and finds nothing. Both protocols miss cleanly; safety invariants hold.",
}
