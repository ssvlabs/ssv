package consensustest

import "time"

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
	Modes: []Mode{ModeCorrectness, ModeStress},
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
		// 2abOBFT (key win): h_V=f selective delivery. f σV verdicts +
		// (N-f-1) NR/NV verdicts at L_0. σ-pool < qV; nr_pool reaches qEnc
		// (since N-1-f honest emit NR via row 5) → NR-quorum at L_0 →
		// advance to L_1 → σ at L_1.
		"2abOBFT": ExpectSuccessFallThrough,
		// QBFT analog: byz R1 leader's PROPOSE reaches only f recipients →
		// PREPARE-pool below qV → R1 round-changes → honest R2 leader
		// (round-robin) succeeds with fresh V. Outcome class = fall-through.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "OBFT-specific deadlock at L_0 (σ-pool=f+1 < qV, NR-pool=2f < qEnc → no L_1 fall-through). 2abOBFT recovers via NR-quorum at L_0. QBFT round-changes from selective-delivery R1 failure to honest R2 leader.",
}

// ---- Late L_0 leader broadcast (Phase 3 — Class A spec) ---------------

var scenarioLateLeaderBroadcast = Scenario{
	Name:  "LateLeaderBroadcast_L0",
	Title: "Late L_0 leader broadcast (past T_commit)",
	Group: "Propagation issues",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		// byz=op1 is L_0 leader by default rotation; broadcasts past T_commit.
		cfg.Byz = ByzPattern{Kind: ByzLateLeaderBroadcast, ByzOperators: []OperatorID{1}}
	},
	Expect: map[string]ExpectClass{
		// OBFT: L_0 σ-pool insufficient (byz bundle past T_commit, honest reject);
		// NR-quorum at L_0 unlocks L_1 → honest L_1 leader broadcasts on time → fall-through.
		"OBFT": ExpectSuccessFallThrough,
		// 2abOBFT: same — bundle arrives past T_commit at honest receivers
		// (rejected); NR-quorum at L_0 → advance to L_1 → σ at L_1.
		"2abOBFT": ExpectSuccessFallThrough,
		// QBFT analog: byz R1 leader's PROPOSE arrives past R1's timer
		// (functionally equivalent to silent leader) → R1 round-changes →
		// honest R2 leader succeeds with fresh V.
		"QBFT": ExpectSuccessFallThrough,
	},
	Note: "Class A spec test (asymmetric propagation past T_commit). OBFT-family falls through to a deeper layer via the per-layer absorption window; QBFT round-changes to an honest R2 leader.",
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
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// f mesh-flaky ops at op2..op{f+1}: 2·BTT inbound delay.
		flakyOverrides := make(map[OperatorID]time.Duration, f)
		for i := 0; i < f; i++ {
			flakyOverrides[OperatorID(i+2)] = 2 * cfg.BTT
		}
		// Wrap whatever Network the profile set (LogNormal in stress,
		// ConstantDelay in correctness) so non-flaky receivers still see
		// the profile's variance. The slow-op overrides are absolute —
		// PerReceiverDelay returns them as-is, so the algebraic miss
		// conditions on flaky ops stay deterministic.
		inner := cfg.Network
		if inner == nil {
			inner = ConstantDelay{D: cfg.BTT}
		}
		cfg.Network = PerReceiverDelay{
			Inner:     inner,
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
		// 2abOBFT: same algebraic shape — flaky honest can't retain V at
		// L_0 by Phase-2a → NR verdicts; byz σ-refusal contributes nothing.
		// nr_pool and σ-pool both short of quorum at L_0; walk fall-through
		// hits same shape at deeper layers (flaky ops still slow). MISS
		// cleanly. Same outcome as OBFT.
		"2abOBFT": ExpectMiss,
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
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// op2..op{f+1}: 3·BTT inbound delay. Pushes Phase-1 bundle arrival
		// past T_commit at those ops; σ-pool retains the remaining
		// (N-1-f) on-time honest non-leaders + leader's σ_L^V = N-f = qV.
		overrides := make(map[OperatorID]time.Duration, f)
		for i := 0; i < f; i++ {
			overrides[OperatorID(i+2)] = 3 * cfg.BTT
		}
		// See MeshFlakiness for the inner-preservation rationale.
		inner := cfg.Network
		if inner == nil {
			inner = ConstantDelay{D: cfg.BTT}
		}
		cfg.Network = PerReceiverDelay{
			Inner:     inner,
			Overrides: overrides,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool = N-f = qV at L_0; decides at L_0 with the slow ops
		// NR'd in their local commits but irrelevant to cluster σ-quorum.
		"OBFT": ExpectSuccessFastest,
		// 2abOBFT: σ-pool = N-f at L_0 from on-time receivers; verdict pool
		// reaches qV at honest receivers → σ-quorum at L_0. Same outcome.
		"2abOBFT": ExpectSuccessFastest,
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
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		f := cfg.F()
		// op2..op{f+2}: 3·BTT inbound delay. (f+1) honest slow.
		overrides := make(map[OperatorID]time.Duration, f+1)
		for i := 0; i < f+1; i++ {
			overrides[OperatorID(i+2)] = 3 * cfg.BTT
		}
		// See MeshFlakiness for the inner-preservation rationale.
		inner := cfg.Network
		if inner == nil {
			inner = ConstantDelay{D: cfg.BTT}
		}
		cfg.Network = PerReceiverDelay{
			Inner:     inner,
			Overrides: overrides,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: σ-pool=N-f-1<qV; NR-pool=f+1<qEnc; both short; miss at L_0
		// with no fall-through (chain stays sealed at L_0). Class A
		// asymmetric-propagation-past-T_commit per §Failure modes.
		"OBFT": ExpectMiss,
		// 2abOBFT: same algebraic shape — (f+1) slow non-leaders retain no V;
		// σV verdicts < qV. nr_pool also short. Walk fall-through hits same
		// shape at deeper layers (slow ops persistent) → MISS cleanly.
		"2abOBFT": ExpectMiss,
		// QBFT: R1 PREPARE pool eventually reaches qV once slow ops'
		// late PREPAREs arrive within R1's window (RT=2s). Succeeds at R1.
		// The QBFT-vs-OBFT asymmetry on this exact spec configuration.
		"QBFT": ExpectSuccessFastest,
	},
	Note: "OBFT.md §Liveness / §Failure modes — h_V=1-shape asymmetric propagation. (f+1) honest miss V at T_commit; σ-pool < qV and NR-pool < qEnc; chain stays sealed; OBFT misses. QBFT R1 PREPARE eventually reaches qV from late-arriving slow ops within RT, so R1 succeeds. Pure network-driven (no byz) — distinct from scenarioHV1SelectiveDelivery which is byz-engineered.",
}
