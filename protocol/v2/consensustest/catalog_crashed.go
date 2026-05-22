package consensustest

// Crash-fault scenarios. Distinct from the "Silent operators" group: a
// silent leader there still signs σ as a follower (it's a leader-broadcast
// availability failure), whereas a CRASHED operator is completely offline —
// no leader broadcast, no σ/NR, no certificate, no partial-sig, and it
// receives nothing. Modeled via ByzPattern.Crashed (orthogonal to Kind), so
// a crash composes with a byzantine pattern as long as the total stays ≤ f.
//
// Crashes are a pure liveness fault (a fully-silent op can never help a
// byzantine aggregator forge a second V), so expectations are Success /
// SuccessOrMiss — never a safety violation.

// ---- Primary (L_0 / R1) leader crash -----------------------------------

var scenarioPrimaryLeaderCrash = Scenario{
	Name:  "PrimaryLeaderCrash",
	Title: "Primary leader crashed (fully offline)",
	Group: "Crashed operators",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		// op1 leads L_0 (OBFT/2abOBFT) and R1 (QBFT) at every cluster size.
		cfg.Byz = ByzPattern{Crashed: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		// Unlike the still-signs SilentLeader, the crashed leader contributes
		// no σ anywhere, so the surviving N−1 honest ops carry the σ-quorum
		// at the backup layer / round they recover into.
		"OBFT":    ExpectSuccessFallThrough, // L_0 dead → in-round fall-through to L_1
		"2abOBFT": ExpectSuccessFallThrough, // same recovery shape as OBFT
		"QBFT":    ExpectSuccessFallThrough, // R1 dead → round-change to R2 (pays RT)
		// PSigs has no leader — a crashed op is just one fewer of the 2f+1
		// partial-sig sources; the rest still reach qV at the fastest path.
		"PSigs": ExpectSuccessFastest,
	},
	Note: "Primary leader completely offline (vs. SilentLeader, which still signs as a follower). OBFT/2abOBFT fall through to the next layer; QBFT round-changes; PSigs is leaderless so the remaining signers still reach qV. At n=4 (f=1) the survivors' σ-pool is exactly qV — the tight-margin liveness case.",
}

// ---- Non-leader crash --------------------------------------------------

var scenarioCrashNonLeader = Scenario{
	Name:  "CrashNonLeader",
	Title: "Non-leader operator crashed (fully offline)",
	Group: "Crashed operators",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		// Crash the highest-id operator — never the L_0/R1 leader (op1), so
		// the healthy fastest path holds. op{N} is the deepest-layer leader
		// only at K=N (never reached on the healthy path) and leads no layer
		// at K<N; either way the L_0 quorum forms without it.
		cfg.Byz = ByzPattern{Crashed: []OperatorID{OperatorID(cfg.N)}}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest, // L_0 leader up + N−1 honest signers ≥ qV
		"2abOBFT": ExpectSuccessFastest,
		"QBFT":    ExpectSuccessFastest, // R1 leader up → decide at R1
		"PSigs":   ExpectSuccessFastest, // N−1 ≥ qV signers
	},
	Note: "A single non-leader fully offline. The L_0/R1 leader is up, so all protocols decide at the fastest path with the remaining N−1 honest signers (≥ qV for n ≥ 4).",
}

// ---- Crash + byzantine combination (≤ f total) -------------------------

var scenarioCrashPlusByz = Scenario{
	Name:  "CrashPlusByzRefusal",
	Title: "Crash + byzantine σ-refusal (≤ f total)",
	Group: "Crashed operators",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		// Crash composes with a byzantine pattern only when the budget has
		// room for both: f ≥ 2 (n ≥ 7). At f = 1 (n = 4) the combination
		// can't fit, so this degrades to a pure non-leader crash — keeping
		// the scenario valid (≤ f) at every cluster size rather than gating
		// it out. The byz half is σ-refusal (a non-leader that never
		// contributes), which every protocol family translates, so the
		// combo runs on OBFT / 2abOBFT / QBFT / PSigs alike.
		crash := OperatorID(cfg.N) // highest id, non-L_0-leader
		if cfg.F() >= 2 {
			cfg.Byz = ByzPattern{
				Kind:         ByzSigmaRefusal,
				ByzOperators: []OperatorID{2},
				Crashed:      []OperatorID{crash},
			}
			return
		}
		cfg.Byz = ByzPattern{Crashed: []OperatorID{crash}}
	},
	Expect: map[string]ExpectClass{
		// At f ≥ 2 the cluster loses two of its 2f+1 σ sources (one crashed,
		// one refusing), leaving exactly qV honest contributors — a tight
		// margin whose success is timing-dependent under degraded transport.
		// At f = 1 it degrades to a single non-leader crash (clean success).
		// SuccessOrMiss accepts both regimes.
		"OBFT":    ExpectSuccessOrMiss,
		"2abOBFT": ExpectSuccessOrMiss,
		"QBFT":    ExpectSuccessOrMiss,
		"PSigs":   ExpectSuccessOrMiss,
	},
	Note: "Crash + byzantine σ-refusal within the f-budget. Only a genuine combination at f ≥ 2 (n ≥ 7), where one crashed + one refusing op leaves exactly qV honest σ sources; at n=4 (f=1) it degrades to a single non-leader crash. Pure liveness — safety holds in both regimes.",
}
