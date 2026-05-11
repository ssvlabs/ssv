package consensustest

// Catalog is the canonical list of cross-protocol scenarios. Each Scenario
// declares a SimConfig modifier and per-protocol expectations sourced from
// docs/BFT-comparison.md and docs/OBFT.md §Application.
//
// Scenarios are ordered roughly by failure-mode taxonomy: healthy →
// propagation issues → silent operators → equivocation patterns →
// validity divergence → OBFT-specific patterns (n/a for QBFT).
//
// Tiers: every catalog scenario opts into BOTH ModeCorrectness and
// ModeStress (see docs/CONSENSUSTEST-SPLIT-PLAN.md). The Apply functions
// are pure config mutators (no randomness, no in-body assertions). Each
// tier runs them with a different network model:
//   - TestCorrectness uses CorrectnessProfile (ConstantDelay): outcomes
//     are deterministic, the test asserts the declared outcome class.
//   - TestStress runs the curated DefaultSweeps at a single cluster
//     size (CLUSTER_SIZE env, default 4). Every sweep uses
//     LogNormalDelay as its propagation model (production-shaped per
//     OBFT.md §Setting): p2p_ideal at σ=0.1 (low-noise control),
//     p2p_normal at σ=0.5 (production baseline), p2p_increasing_BTT and
//     p2p_heavy_tail varying BTT and σ along their axes, p2p_packet_loss
//     wrapping LossyNetwork over the production baseline, and
//     p2p_correlated_delays wrapping CorrelatedLinkDelay over the same.
//     Many iterations; stats only. Cross-cluster-size comparison is done
//     by re-running TestStress with different CLUSTER_SIZE values, not
//     by a dedicated sweep.
//
// A scenario that needs a specific network shape (e.g. MeshFlakiness's
// PerReceiverDelay, AsymmetricPropagation_*'s per-receiver overrides)
// overrides the tier's Network in its Apply.
//
// Add new scenarios at the end to keep order stable for matrix-rendering.
//
// Cluster-size scope: TestCorrectness runs at n=4 (the canonical
// operating point per OBFT.md §Application). The larger-n sweep
// (TestSweep_FullCatalog_LargerN in sweep_test.go) exercises every
// scenario at n ∈ {7, 10, 13} too, enforcing universal safety invariants
// and asserting per-cell expectation matches. Every catalog scenario
// produces the same outcome class at all SSV cluster sizes — Apply
// functions scale with cfg.N / cfg.F() so f-quorum mechanics are
// preserved.
//
// Generalization key: scenarios named with a number suffix (e.g. "_3_1",
// "_AlgebraicLimit", "_NRFallThrough", "_NaturalRecovery") describe the
// f=1 / n=4 instance of a behavior class. The Apply scales the count of
// affected operators (NV count, recipient count) with cfg.F() so the
// behavior class holds at any n.
var Catalog = []Scenario{
	// Baseline
	scenarioHealthy,

	// Propagation issues
	scenarioHV1SelectiveDelivery,
	scenarioLateLeaderBroadcast,
	scenarioAsymmetricPropagation_FSlow_Success,
	scenarioAsymmetricPropagation_FPlus1Slow_Miss,
	scenarioMeshFlakiness,

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

	// OBFT-specific attacks
	scenarioCrossSigningRule1,
	scenarioCrossOnionEquivocationRule3,
	scenarioFakeEncryptedPresence,
	scenarioFakePlaintextSigmaRule5,
}
