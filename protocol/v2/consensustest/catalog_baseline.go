package consensustest

import "time"

// ---- Healthy ------------------------------------------------------------

var scenarioHealthy = Scenario{
	Name:  "Healthy",
	Title: "Healthy under production-realistic mesh",
	Group: "Baseline",
	Modes: []Mode{ModeCorrectness, ModeStress},
	// Healthy is the only catalog scenario that exercises the full
	// production-mesh stack: libp2p-shaped eager-push (DeliveryMesh,
	// per-hop dedup + reflood) plus the lazy-push IHAVE/IWANT gossip
	// backstop (MeshGossipConfig.Enabled) plus the matching
	// protocol-side reflood-absorption budget (cfg.RefloodDelay =
	// 700ms, mirroring SSV's libp2p heartbeat and the production SSV
	// adapter's DefaultRefloodDelay). Adversarial scenarios
	// (selective broadcast, asymmetric byz patterns, host-flip cases)
	// stay on DeliveryDirect because their per-(from, to) primitives
	// lose meaning under reflood. WrapBaselineForInstability composes
	// onto Healthy's Apply — instability variants inherit RefloodDelay
	// + Mesh.Gossip settings unchanged, then layer slow-op + loss
	// wrappers on top of cfg.Network and cfg.Mesh.HopDelay (sub-field
	// writes only; no wholesale rebuild of cfg.Mesh).
	Delivery: DeliveryMesh,
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzNone}
		cfg.Host = HostAllValid{}
		// Production-realistic-mesh modeling: enable libp2p's lazy-push
		// gossip backstop (defaults match SSV's libp2p — heartbeat
		// 700ms, mcache 6 slots / 4 gossip — see MeshGossipConfig)
		// and size the protocol's primary-layer broadcast schedule
		// (B_0 = 2·BTT + RefloodDelay) for the same 700ms reflood
		// absorption window. Mirrors the production SSV adapter's
		// DefaultRefloodDelay.
		cfg.Mesh.Gossip.Enabled = true
		cfg.RefloodDelay = 700 * time.Millisecond
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest,
		"2abOBFT": ExpectSuccessFastest,
		"QBFT":    ExpectSuccessFastest,
		"PSigs":   ExpectSuccessFastest,
	},
	Note: "Protocols complete at fastest path under all-honest, production-realistic libp2p mesh — eager-push + lazy-push IHAVE/IWANT gossip backstop, with a RefloodDelay-aware primary broadcast schedule. BFT-comparison.md Table 1.",
}
