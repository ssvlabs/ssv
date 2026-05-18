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
		// OBFT: recovers at L_0 via peer-reflood-V per OBFT.md §Phase 2.
		// The 1 honest V-recipient early-emits with V plaintext + σ_L^V
		// witness in their KindCommit; V-drop receivers harvest V from
		// the σ-onion entry, verify σ_L^V, host-validate V, and σ_i^V on
		// it in their own (later) commit. σ-pool = leader σ_L^V + 3
		// honest σ = 4 ≥ qV=3. Slot succeeds at L_0. Closes the historical
		// h_V=1 deadlock that pre-§1 OBFT had no in-protocol recovery for.
		"OBFT": ExpectSuccessFastest,
		// 2abOBFT (key win): h_V=f selective delivery. f σV verdicts +
		// (N-f-1) NR/NV verdicts at L_0. σ-pool < qV; nr_pool reaches qEnc
		// (since N-1-f honest emit NR via row 5) → NR-quorum at L_0 →
		// advance to L_1 → σ at L_1.
		"2abOBFT": ExpectSuccessFallThrough,
		// QBFT analog: byz R1 leader's PROPOSE reaches only f recipients →
		// PREPARE-pool below qV → R1 round-changes → honest R2 leader
		// (round-robin) succeeds with fresh V. Outcome class = fall-through.
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no leader to selectively-deliver
	},
	Note: "OBFT closes the historical h_V=1 deadlock in-protocol via OBFT.md §Phase 2 peer-reflood-V (V-drop receivers σ on V harvested from the in-time recipient's KindCommit σ-onion + σ_L^V witness) combined with the L0Ready-driven evtCommitEmit framework upgrade. The 1 V-recipient early-emits with V+σ_L^V; V-drop receivers σ on peer-V. 2abOBFT recovers via NR-quorum at L_0 → fall-through to L_1. QBFT round-changes from selective-delivery R1 failure to honest R2 leader.",
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
		"QBFT":  ExpectSuccessFallThrough,
		"PSigs": ExpectNotApplicable, // PSigs has no leader-broadcast phase
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
		// f mesh-flaky ops at op2..op{f+1}: 2·BTT inbound delay. Originally
		// sized to exceed the pre-reflood-aware OBFT B_0 = 1·BTT (causing
		// the OBFT mesh-flakiness deadlock the scenario was designed to
		// document). Under the reflood-aware primary-vs-backup schedule
		// (B_0 = 2·BTT + RefloodDelay for the primary; this scenario
		// keeps the framework default RefloodDelay=0 since it runs on
		// DeliveryDirect — its per-(from, to) primitives lose meaning
		// under mesh-reflood, so the production-mesh opt-in only fires
		// on Healthy), so B_0 = 2·BTT and this 2·BTT delay no longer
		// exceeds the primary's absorption window — the scenario now
		// documents the *improvement* from the wider primary B_0 rather
		// than the legacy deadlock. The QBFT/PSigs success expectations
		// remain unchanged. To exercise the legacy deadlock pattern,
		// use a deeper-flakiness scenario (delay > B_0 + Δ_2a + reflood-
		// cycle).
		//
		// PROFILE-INVARIANCE: the override is intentionally BTT-coupled,
		// not anchored to cfg.Network.SlowOpAnchor(). The flakiness
		// boundary the scenario tests is a *protocol-budget* relation
		// (flaky arrival vs T_commit, both BTT-derived); pinning the
		// delay to 2·BTT keeps the algebraic boundary identical across
		// every p2p_baseline profile at a given BTT. Consequence on the
		// heatmap: this scenario's row is approximately constant across
		// the profile axis at fixed BTT — the empirical-profile axis
		// does NOT differentiate flakiness behaviour here, by design.
		// Use the instability / loss / correlated-delay sweeps for
		// profile-sensitive flakiness exploration.
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
		// OBFT: under the reflood-aware primary B_0 = 2·BTT + RefloodDelay
		// (this scenario keeps RefloodDelay=0 default, so B_0 = 2·BTT),
		// 2·BTT flaky-receiver delay is absorbed by L_0's primary window
		// — flaky ops still receive V on time and σ-emit. σ-pool =
		// N-f-byz_silent reaches qV at L_0. **Improvement** vs the
		// pre-resize schedule where 2·BTT delay would have exceeded the
		// old B_0 = 1·BTT and triggered the deadlock.
		"OBFT": ExpectSuccessFastest,
		// 2abOBFT: still misses, but via a different shape than under the
		// pre-resize schedule. Flaky receivers DO observe V at L_0 (Phase-1
		// bundle arrives within Phase-2a window at the new B_0), but peer
		// VERDICTS arrive late at the flaky op too (2·BTT inbound delay
		// applies to all messages). The flaky op computes Phase-2b
		// convergence on a partial verdict-pool, hits σ-eligibility-
		// quorum-short, and NR-flips per the convergence rule. Combined
		// with byz σ-refusal, σ-pool stays short and NR-pool falls short
		// of qEnc — slot misses at L_0 with no fall-through.
		"2abOBFT": ExpectMiss,
		// QBFT: flaky receivers see PROPOSE/PREPAREs with delay but non-flaky
		// non-byz honest count (N-1-2f) PREPARE among themselves on time;
		// quorum (qV=2f+1) reaches at R1 once flaky ops' delayed PREPAREs
		// arrive. Decides at R1 across all SSV cluster sizes — the
		// QBFT-vs-OBFT-family asymmetry the spec calls out under mesh
		// flakiness (PREPARE-pool quorum-by-arrival vs OBFT's hard T_commit
		// cutoff with no late retention).
		"QBFT": ExpectSuccessFastest,
		// PSigs: byz σ-refusal removes one signer; flaky-receiver delays
		// affect that op's inbound only — outbound partials from flaky
		// ops still arrive at non-flaky receivers on time. N-1-f = 2f
		// honest signers (excluding byz) each emit one partial; non-flaky
		// receivers gather qV from on-time arrivals. Decides at L_0-
		// equivalent (BFTStart + 1·BTT). PSigs has no σ-or-NR
		// convergence rule, so the mesh-flakiness deadlock that traps
		// OBFT-family doesn't apply — this cell makes that asymmetry
		// visible on the heatmap.
		"PSigs": ExpectSuccessFastest,
	},
	Note: "OBFT.md §Properties / Mesh-flakiness tolerance: flaky honest NR-emits incorrectly + byz σ-refusal → OBFT both quorums short → no fall-through (miss). QBFT recovers at R1 (PREPARE-pool reaches qV once delayed flaky PREPAREs arrive — no hard cutoff). PSigs decides naturally (no σ-or-NR split → no flakiness deadlock). Validates the spec's 'mesh-flaky honest = f-budget consumer' claim and the QBFT-vs-OBFT asymmetry.",
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
// bundle for L_0 arrives at op2 late enough to land past T_commit at op2
// (scenario fixture sized so op2's bundle misses its accept horizon while
// the other 3 operators still observe in time). σ-pool at L_0 =
// op1(leader) + op3 + op4 = 3 = qV. Cluster decides at L_0.
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
		//
		// PROFILE-INVARIANCE: BTT-coupled by design — the scenario tests
		// a protocol-budget boundary (flaky arrival vs T_commit, both
		// BTT-derived), so the override magnitude is pinned to 3·BTT
		// across every profile at fixed BTT to keep the algebraic
		// boundary identical. See MeshFlakiness for the same note.
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
		// PSigs: V is pre-agreed, so receiver-side delays don't keep an
		// op from signing. Slow ops still emit partials (their outbound
		// path is not throttled); fast non-slow receivers reach qV at
		// BFTStart + 1·BTT from on-time peer partials.
		"PSigs": ExpectSuccessFastest,
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
		//
		// PROFILE-INVARIANCE: BTT-coupled by design (see MeshFlakiness
		// and AsymmetricPropagation_FSlow_Success). The miss boundary is
		// a protocol-budget relation, pinned identically across profiles.
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
		// QBFT: outcome is timing-dependent now that the framework
		// models post-consensus partial-sig aggregation explicitly.
		// At n=4 the fast subset yields only N−f−1=2 partial sigs and
		// the slow ops can't reach consensus + emit partials within
		// the slot — cluster misses. At larger n (f+1 slow non-leaders
		// is still a minority) the fast subset itself can hit 2f+1
		// quickly enough, or some slow ops decide in time to push the
		// quorum over. The previously-asserted "QBFT always succeeds
		// here" expectation was an artifact of the fixed PhaseBudget
		// approximation; with real partial-sig events the outcome
		// genuinely depends on cluster size and propagation luck, so
		// ExpectSuccessOrMiss is the honest catalog entry.
		"QBFT": ExpectSuccessOrMiss,
		// PSigs: same intuition as the f-slow variant — V is pre-agreed
		// so receiver-side delays don't gate signing. Slow ops' outbound
		// partials still arrive at fast receivers on time; fast receivers
		// reach qV at BFTStart + 1·BTT. The OBFT-family miss here
		// depends on the σ-or-NR split which PSigs doesn't have.
		"PSigs": ExpectSuccessFastest,
	},
	Note: "OBFT.md §Liveness / §Failure modes — h_V=1-shape asymmetric propagation. (f+1) honest miss V at T_commit; σ-pool < qV and NR-pool < qEnc; chain stays sealed; OBFT misses. QBFT outcome is timing-dependent with Phase-C post-consensus modelling: at n=4 the fast subset is too small to hit 2f+1 partials in time and the cluster misses; at larger n the fast subset alone can reach quorum. PSigs decides at BFTStart + 1·BTT (V pre-agreed → receiver delays don't gate signing). Pure network-driven (no byz) — distinct from scenarioHV1SelectiveDelivery which is byz-engineered.",
}
