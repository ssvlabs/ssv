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
// ModeStress. The Apply functions
// are pure config mutators (no randomness, no in-body assertions). Each
// tier runs them with a different network model:
//   - TestCorrectness uses CorrectnessProfile (ConstantDelay): outcomes
//     are deterministic, the test asserts the declared outcome class.
//   - TestStress runs the seven curated DefaultSweeps at a single
//     (n, K) operating point across the OBFT, 2abOBFT, and QBFT
//     protocol families (cushion-ladder + fixed-RT variants — see
//     stress_test.go for the full registered list). p2p_baseline
//     uses calibrated empirical mesh-hop profiles fitted to SSV
//     prod / stage gossipsub telemetry; the synthetic-degradation
//     sweeps retain LogNormal anchors so their parameter axis stays
//     meaningful:
//     p2p_baseline           (BTT × profile × instability cross-product)
//     p2p_increasing_BTT     (BTT axis, LogNormal{Median: BTT/2, σ: 0.5})
//     p2p_packet_loss        (LossyNetwork × LossRate axis)
//     p2p_partitions         (MeshConfig.SeverProb × per-pair sustained severance)
//     p2p_correlated_delays  (CorrelatedLinkDelay × BadLinkProb axis)
//     p2p_node_slowness      (MarkovianSlownessDelay × slow-op count)
//     p2p_instability        (Healthy-only instability-level curve)
//     Many iterations (baseline / unstable split); stats only. Cross-
//     (n, K) comparison is done by re-running TestStress with different
//     CLUSTER_SIZES_N / LAYERS_K values, not by a dedicated sweep.
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

	// Crashed operators (fully offline; ByzPattern.Crashed)
	scenarioPrimaryLeaderCrash,
	scenarioCrashNonLeader,
	scenarioCrashPlusByz,

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
	scenarioPhase2EquivocateCrossV_Rule6a,
	scenarioPhase2DowngradeValueNoValue_Rule6a,
}
