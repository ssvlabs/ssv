package obft_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
)

// TestMeshArrival_NoRefloodToPublisher pins the publisher-exclusion
// contract: real go-libp2p-pubsub's Publish skips both the relay sender
// and msg.GetFrom() (the original publisher) when forwarding through
// the mesh — the sim mirrors that, so no MeshArrival in the trace
// should be scheduled with `to == publisher`, even on multi-hop
// reflood paths that loop back toward the publisher's neighborhood.
// (The dedup cache used to mask the bug by dropping the loop-back
// arrival at handle time, so a scheduling-side assertion is the right
// level.)
func TestMeshArrival_NoRefloodToPublisher(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	cfg.TraceEnabled = true
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	ct.AssertNoRefloodToPublisher(t, out.Trace)
}

// TestMeshGossip_SmokeOBFT exercises the Phase B gossip layer on top
// of the existing healthy-mesh path: gossip enabled with SSV defaults
// (700ms heartbeat, 4-slot IHAVE window, etc.), TraceEnabled so the
// gossip events show up in out.Trace, and we assert the run still
// decides AND the trace contains MeshHeartbeat / MeshIHave entries.
// (At Healthy the eager mesh is fast enough on its own — IWANT may or
// may not fire depending on heartbeat phasing vs publish timing, so
// we don't assert on it here.)
func TestMeshGossip_SmokeOBFT(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	cfg.Mesh.Gossip = ct.MeshGossipConfig{Enabled: true}
	cfg.TraceEnabled = true
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "gossip-enabled healthy mesh should decide")

	var heartbeats, ihaves int
	for _, e := range out.Trace {
		switch {
		case strings.HasPrefix(e.Event, "MeshHeartbeat["):
			heartbeats++
		case strings.HasPrefix(e.Event, "MeshIHave["):
			ihaves++
		}
	}
	require.Positive(t, heartbeats, "trace should contain heartbeats once gossip is enabled")
	require.Positive(t, ihaves, "trace should contain IHAVE entries after publishers populate their mcaches")
	t.Logf("gossip smoke: %d heartbeats, %d IHAVE events", heartbeats, ihaves)
}

// TestMeshGossip_SlowMeshRescue_OBFT demonstrates the value of the
// lazy-push backstop: HopDelay is configured slow enough that the
// eager mesh can't deliver Phase-1 / Commits before the OBFT decision
// fires — without gossip, the cluster misses the slot. With gossip
// enabled (Network direct delays << HopDelay; HeartbeatInterval short
// enough to fit several ticks inside the slot), IHAVE/IWANT carries
// messages on direct connections and the cluster decides in time.
//
// Calibration: HopDelay = 5s pushes every mesh hop past the 4s
// RelayCutoff. Network = 50ms direct + HeartbeatInterval = 100ms
// gives ~4 heartbeats inside the OBFT Phase-1 → Phase-3 window, so
// gossip can cascade through the cluster within Δ_2. SSV-realistic
// defaults (HopDelay ~BTT/3, HeartbeatInterval 700ms) don't expose
// the gap nearly as cleanly — that's a tuning regime, not the
// mechanism this test exercises.
func TestMeshGossip_SlowMeshRescue_OBFT(t *testing.T) {
	build := func(gossip bool) ct.SimConfig {
		btt := 200 * time.Millisecond
		cfg := ct.SimConfig{
			N:            4,
			Operators:    ct.MakeOperators(4),
			SlotDuration: 12 * time.Second,
			RelayCutoff:  4 * time.Second,
			BTT:          btt,
			Network:      ct.ConstantDelay{D: 50 * time.Millisecond},
			Byz:          ct.ByzPattern{Kind: ct.ByzNone},
			Seed:         1,
			Delivery:     ct.DeliveryMesh,
			Mesh: ct.MeshConfig{
				HopDelay: ct.ConstantDelay{D: 5 * time.Second},
				Gossip: ct.MeshGossipConfig{
					Enabled:           gossip,
					HeartbeatInterval: 100 * time.Millisecond,
				},
			},
		}
		return cfg
	}
	noGossip, err := obftadapter.Protocol{}.Run(build(false))
	require.NoError(t, err)
	withGossip, err := obftadapter.Protocol{}.Run(build(true))
	require.NoError(t, err)

	t.Logf("no-gossip: decided=%v at %v; with-gossip: decided=%v at %v",
		noGossip.Decided, noGossip.DecisionTime, withGossip.Decided, withGossip.DecisionTime)

	require.False(t, noGossip.Decided,
		"without gossip, HopDelay=5s mesh can't deliver Phase-1/Commits before the clip deadline")
	require.True(t, withGossip.Decided,
		"with gossip enabled, IHAVE/IWANT on the fast direct path delivers messages in time")
}

// TestRecovery_PeerVOnHV1 pins the §1 peer-reflood-V recovery at the
// adapter level. HV1SelectiveDelivery sends Phase-1 V to exactly f
// honest operators; pre-§1 this deadlocked at L_0 (σ-pool short of qV,
// NR-pool short of qEnc → no L_1 fall-through). Under §1, V-drop
// receivers harvest V from the in-time recipients' KindCommit σ-onion
// at L_0, host-validate, and σ on it — closing the gap so σ-pool
// reaches qV and the cluster decides at L_0.
//
// Classifier-label correctness (the prior assertion target) is
// independently covered by classifyOBFTMiss in adapter_internal_test.go.
func TestRecovery_PeerVOnHV1(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// HV1SelectiveDelivery at n=4: byz leader (op1) delivers V to
	// exactly f=1 honest (op2). The other two honest receive nothing
	// from the leader's Phase-1 — they recover V from op2's KindCommit
	// σ-onion entry at L_0 (§1 peer-reflood-V path).
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzHV1SelectiveDelivery,
		ByzOperators: []ct.OperatorID{1},
		Recipients:   []ct.OperatorID{2},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "HV1SelectiveDelivery should recover via §1 peer-reflood-V")
	require.Equal(t, 0, out.DecidedRound, "recovery happens at L_0, not via fall-through")
}

// TestRecovery_PeerVOnHV1_DegradedBTT pins the peer-reflood-V recovery
// (OBFT.md §Phase 2 / Peer-reflood V via early commit) across the SSV
// operational BTT envelope. Pinned regression: V-drop receiver's
// L0Ready must fire comfortably before T_commit fallback even at
// degraded BTT (e.g., BTT=600ms), otherwise the receiver NR-locks
// before harvesting V from the in-time recipient's commit and the
// recovery doesn't fire.
//
// Timing chain at each BTT: L_0 leader emits Phase-1 at FetchAt[0];
// V-recipient (op 2) receives Phase-1 at FetchAt[0]+BTT, L0Ready
// closes via Phase-1-retention σ path → emits commit at that moment;
// V-drop receivers (op 3, op 4) receive op 2's commit at
// FetchAt[0]+2·BTT, harvest V via the peer-V path, drain host
// validation, L0Ready closes → emit commit at FetchAt[0]+2·BTT;
// σ-quorum reaches when those reach the cluster at FetchAt[0]+3·BTT.
//
// Recovery requires FetchAt[0]+2·BTT < T_commit so V-drop receivers
// emit BEFORE the evtPhaseTwoStart T_commit fallback locks them into
// NR. At BTT ≥ ~1000ms (with the framework's Δ_2 = 2·BTT conservatism)
// T_commit shrinks below the propagation chain — outside the recovery
// envelope. Production uses Δ_2 = 1·BTT, where the envelope reaches
// further; the framework values bound the conservative case.
func TestRecovery_PeerVOnHV1_DegradedBTT(t *testing.T) {
	// Operational SSV BTT envelope: 100ms (LAN-fast) to 600ms (degraded
	// WAN). The §6 plan question explicitly calls out BTT=600ms.
	for _, btt := range []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		600 * time.Millisecond,
	} {
		t.Run(btt.String(), func(t *testing.T) {
			cfg := ct.DefaultProposerDutyConfig(btt)
			cfg.Byz = ct.ByzPattern{
				Kind:         ct.ByzHV1SelectiveDelivery,
				ByzOperators: []ct.OperatorID{1},
				Recipients:   []ct.OperatorID{2},
			}
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err)
			require.True(t, out.Decided,
				"BTT=%v: HV1 should recover via §1 peer-reflood-V; missReason=%q",
				btt, out.MissReason)
			require.Equal(t, 0, out.DecidedRound,
				"BTT=%v: recovery should land at L_0, not via fall-through", btt)
			t.Logf("BTT=%v: recovered at L_0 in %v", btt, out.DecisionTime)
		})
	}
}

// TestMeshBandwidth_NoPhantomRelayOperator — in DeliveryMesh mode the
// cluster's PerOperatorIn metric must not accumulate any bytes against
// OperatorID(0). The sentinel-0 receiver was previously created by
// charging relay-bound bytes through Emission, polluting the per-op
// inbound histogram. Phase B's EmissionToRelay split fixed that;
// pin the contract here.
func TestMeshBandwidth_NoPhantomRelayOperator(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Delivery = ct.DeliveryMesh
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	require.Zero(t, out.Bandwidth.PerOperatorIn[0],
		"PerOperatorIn[0] must stay zero — relay-bound bytes should not pollute the per-operator histogram")
	// Sanity: cluster operators 1..N should have non-zero inbound.
	for op := ct.OperatorID(1); op <= 4; op++ {
		require.Positive(t, out.Bandwidth.PerOperatorIn[op],
			"cluster operator %d expected non-zero PerOperatorIn", op)
	}
}

// TestCalibration_MeshVsDirect_Healthy_OBFT validates Phase B's
// calibration anchor: the default HopDelay = LogNormal{Median: BTT/3,
// Sigma: 0.3} lands mesh-mode Healthy at the same OBFT outcome class
// (decided at L_0) as direct-mode Healthy, with mesh-mode wire
// bandwidth visibly larger (extra hops × relay reflood).
//
// Calibration target: bandwidth ratio ≥ 2× direct (mesh has D × hops
// fanout vs direct's n-1). At n=4 with D=3 and ~2 hops, expect ~3-4×.
// Success rate must match (both 100%) — OBFT's Phase 3 resolve fires
// at a fixed schedule offset, so DecisionTime itself doesn't
// differentiate the modes at Healthy.
func TestCalibration_MeshVsDirect_Healthy_OBFT(t *testing.T) {
	const iters = 20
	btt := 200 * time.Millisecond
	base := ct.DefaultProposerDutyConfig(btt)
	// Match the stress matrix's production-shaped LogNormal default so
	// the direct-mode anchor reflects what stress runs see.
	base.Network = ct.LogNormalDelay{Median: btt / 2, Sigma: 0.5}

	runMode := func(t *testing.T, delivery ct.DeliveryMode) (decided int, totalBytes int64) {
		for i := 0; i < iters; i++ {
			cfg := base
			cfg.Seed = int64(i + 1)
			cfg.Delivery = delivery
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err)
			if out.Decided {
				decided++
			}
			totalBytes += out.Bandwidth.TotalBytes
		}
		return
	}
	directDecided, directBW := runMode(t, ct.DeliveryDirect)
	meshDecided, meshBW := runMode(t, ct.DeliveryMesh)
	t.Logf("direct: %d/%d decided, total bandwidth %d B", directDecided, iters, directBW)
	t.Logf("mesh:   %d/%d decided, total bandwidth %d B", meshDecided, iters, meshBW)

	require.Equal(t, iters, directDecided, "direct healthy must decide every iter")
	require.Equal(t, iters, meshDecided, "mesh healthy must decide every iter")
	// Mesh bandwidth should be meaningfully larger than direct (extra
	// hops × relay reflood). Conservative lower bound: 1.5× — well below
	// the expected 3-4× but tolerant of per-seed variance and the
	// publish-only outbound accounting (forwards from relays don't add
	// to bandwidth; see emitMesh comment).
	require.Greater(t, meshBW, int64(float64(directBW)*1.5),
		"mesh bandwidth (%d B) should be at least 1.5× direct (%d B)", meshBW, directBW)
}

// TestAdapter_HealthyMesh_N4 runs the healthy path through the mesh
// transport (4 cluster ops + 4 forward-only relays). Asserts the sim
// decides at L_0 just like direct-mode healthy — the mesh transport is
// transparent to the protocol layer, and Phase A's calibration target
// is that mesh-mode Healthy lands roughly at the same outcome as direct
// mode at n=4. Tight tolerance not required here; we just need to
// confirm propagation works end-to-end through the mesh.
func TestAdapter_HealthyMesh_N4(t *testing.T) {
	btt := 200 * time.Millisecond
	cfg := ct.SimConfig{
		N:            4,
		Operators:    ct.MakeOperators(4),
		SlotDuration: 12 * time.Second,
		RelayCutoff:  4 * time.Second,
		BTT:          btt,
		Byz:          ct.ByzPattern{Kind: ct.ByzNone},
		Seed:         1,
		Delivery:     ct.DeliveryMesh,
		Mesh: ct.MeshConfig{
			// Phase A picks BTT/3 as the working calibration anchor
			// (mesh-as-realism: 2-hop typical at n=4 + 4 relays gives
			// cluster-wide P99 ≈ direct-mode BTT). Phase B will tune.
			HopDelay: ct.LogNormalDelay{Median: btt / 3, Sigma: 0.3},
		},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "mesh-mode healthy should decide")
	require.Equal(t, 0, out.DecidedRound, "mesh-mode healthy should decide at L_0 fastest path")
	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("mesh-mode healthy: decided at %v on L_%d", out.DecisionTime, out.DecidedRound)
}

// TestAdapter_OpportunisticDecisionTime — Phase 1 of the
// OBFT-OPPORTUNISTIC-PHASE3 plan, updated for the L0Ready-driven event-
// driven commit emit framework upgrade. Asserts the observer-mode metric
// is active AND that commits fire on L0Ready close (not at T_commit):
// under DeliveryDirect at BTT=200ms (ConstantDelay), σ-quorum at L_0
// reaches at FetchAt[0] + 2·BTT = 3350ms (was 3600ms = T_commit + 1·BTT
// under the prior sync-at-T_commit emit; was 3850ms = RoundEndOffset
// pre-observer-mode). Pinning this 500ms total saving catches both
// regressions: (a) vQuorumAt not being written by the commit-arrival
// path, and (b) the framework's evtCommitEmit not being scheduled on
// L0Ready close.
func TestAdapter_OpportunisticDecisionTime(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "healthy should decide")
	require.Equal(t, 0, out.DecidedRound, "decided at L_0 fastest path")
	// At BTT=200ms: T_commit=3400ms, B_0=2·BTT=400ms, fetchBuffer=BTT/4=50ms
	// → FetchAt[0]=2950ms. L_0 leader emits Phase-1 (and its own early
	// commit) at 2950ms; Phase-1 arrives at peers at 3150ms → ApplyHostValidity
	// closes L0Ready on the σ-retention path → peers early-emit commits at
	// 3150ms → arrivals at 3350ms. The qV-th σ-partial arrival hits σ-quorum
	// at that moment; walk cost at L_0 is 0 (no fall-through). Total saving
	// vs schedule-anchored 3850ms is 500ms (250ms vs sync-emit's 3600ms +
	// another 250ms from the L0Ready-driven early-emit).
	require.Equal(t, 3350*time.Millisecond, out.DecisionTime,
		"observer-mode + L0Ready-driven emit should catch L_0 σ-quorum at FetchAt[0] + 2·BTT = 3350ms "+
			"(was 3600ms under sync-at-T_commit emit; was schedule-anchored 3850ms pre-observer)")
}

// TestAdapter_OpportunisticDecisionTime_Fallthrough is the OBFT-
// OPPORTUNISTIC-PHASE3 plan's "FallbackToScheduleAnchor" test, adapted to
// the observer-mode implementation. The plan was written before observer-
// mode landed and expected DecisionTime to fall back to
// `RoundEndOffset + 1·Epsilon3` in fall-through cases. Under observer-
// mode, the cumulative pool reaches the L_1 σ-quorum at the commit-
// arrival moment too — Resolve walks L_0 (NR-quorum from honest+silent-
// leader NR partials) → L_1 (σ-quorum from honest σ partials) inline on
// every arrival.
//
// What this test pins: the fall-through DecisionTime is T_commit +
// 1·BTT + 1·Epsilon3 (commit-arrival + one-layer-walk cost), NOT the
// schedule-anchored RoundEndOffset + 1·Epsilon3. The 200ms saving for
// fall-through cases is the same opportunistic gap the L_0 healthy
// test pins. The schedule-anchored `resolveOpAndBroadcastCert`
// fallback (which also writes vQuorumAt via RecordFirstOpportunisticQuorum)
// remains as defensive coverage if a future change makes opportunistic
// fail; under the current wiring the opportunistic commit-arrival path
// always beats it.
func TestAdapter_OpportunisticDecisionTime_Fallthrough(t *testing.T) {
	btt := 200 * time.Millisecond
	cfg := ct.DefaultProposerDutyConfig(btt)
	// Silent L_0 leader (op 1): no Phase-1 bundle from op 1 → no honest
	// op retains V at L_0 → all 4 ops emit NR at L_0 in their commits.
	// L_1..L_3 leaders broadcast healthy, so σ partials at those layers
	// flow through commits normally.
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzSilentLeader, ByzOperators: []ct.OperatorID{1}}

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "fall-through to L_1 should decide")
	require.Equal(t, 1, out.DecidedRound, "decided at L_1 (L_0 silent → fall-through)")

	// Opportunistic Resolve fires on each commit arrival; the qV-th arrival
	// (3 of 4 commits) brings σ-pool at L_1 to qV and NR-pool at L_0 to
	// qEnc, satisfying the walk. Layer-walk cost is 1·Epsilon3 (one NR-
	// advance from L_0 to L_1). At BTT=200ms (ConstantDelay), commits arrive
	// at T_commit + 1·BTT = 3600ms; at Epsilon3 = 50ms (DefaultProposerDutyConfig
	// leaves Epsilon3 unset → Validate() applies the default), the
	// vQuorumAt write is 3600 + 50 = 3650ms.
	require.Equal(t, 3650*time.Millisecond, out.DecisionTime,
		"observer-mode fall-through should record vQuorumAt = T_commit + 1·BTT + 1·Epsilon3 = 3650ms "+
			"(NOT the pre-instrumentation schedule-anchored RoundEndOffset + 1·Epsilon3 = 3900ms)")
}

// TestAdapter_HealthyAtClusterSizes verifies the adapter runs healthy at
// every SSV-supported cluster size (n=4,7,10,13). Phase 1 plumbs cfg.K /
// cfg.BroadcastBudget / cfg.FetchAt through the adapter; n != 4 was previously
// untested.
func TestAdapter_HealthyAtClusterSizes(t *testing.T) {
	btt := 200 * time.Millisecond
	for _, n := range ct.ClusterSizes {
		t.Run(clusterName(n), func(t *testing.T) {
			cfg := ct.SimConfig{
				N:            n,
				Operators:    ct.MakeOperators(n),
				SlotDuration: 12 * time.Second,
				RelayCutoff:  4 * time.Second,
				BTT:          btt,
				Byz:          ct.ByzPattern{Kind: ct.ByzNone},
				Seed:         1,
			}
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err, "n=%d Run", n)
			require.True(t, out.Decided, "n=%d should decide healthy", n)
			require.Equal(t, 0, out.DecidedRound, "n=%d should decide at L_0 fastest path", n)

			rep := ct.ComputeSafetyReport(out)
			require.True(t, rep.SingleV, "n=%d SingleV: %s", n, rep)
			require.True(t, rep.NoOfflineDoubleV, "n=%d NoOfflineDoubleV: %s", n, rep)
			t.Logf("n=%d K=%d: decided at %v on L_%d", n, ct.DefaultK(cfg.N), out.DecisionTime, out.DecidedRound)
		})
	}
}

// TestAdapter_MultiByzSilentAtN7 runs at n=7 (f=2) with TWO byz operators
// silent as layer leaders; OBFT decides via NR fall-through to the first
// honest-led layer (L_2 here), and the observer-mode Resolve catches
// quorum at commit-arrival rather than at the schedule anchor.
//
// Pre-observer-mode (when Resolve fired once at RoundEndOffset =
// 3850ms): fall-through to L_2 added 2·ε_3 = 100ms walk cost on top of
// RoundEndOffset → decisionTime = 3950ms > the 3900ms relay-submit
// deadline, so ClipLateDecision converted to MISS. Observer-mode
// (Phase 1 of docs/OBFT-OPPORTUNISTIC-PHASE3-PLAN.md): Resolve runs at
// every commit arrival; at BTT=200ms commits arrive at T_commit + 1·BTT
// = 3600ms; the L_2 σ-walk completes at 3600 + 100ms = 3700ms, inside
// the 3900ms deadline. The new semantic correctly reflects that
// production observer-mode runners would NOT miss this scenario.
func TestAdapter_MultiByzSilentAtN7(t *testing.T) {
	btt := 200 * time.Millisecond
	cfg := ct.SimConfig{
		N:            7,
		Operators:    ct.MakeOperators(7),
		SlotDuration: 12 * time.Second,
		RelayCutoff:  4 * time.Second,
		BTT:          btt,
		Byz: ct.ByzPattern{
			Kind:         ct.ByzSilentLeader,
			ByzOperators: []ct.OperatorID{1, 2},
		},
		Seed: 1,
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "n=7 with 2 byz silent leaders: observer-mode opportunistic Resolve catches L_2 σ-quorum at commit arrival (3600ms + 2·ε_3 = 3700ms), well inside the 3900ms submit deadline")
	require.Equal(t, 2, out.DecidedRound, "decided at L_2 — first honest-led layer past the two byz-led ones")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("n=7 K=%d 2-byz-silent: decided at %v on L_%d via observer-mode opportunistic Resolve",
		ct.DefaultK(cfg.N), out.DecisionTime, out.DecidedRound)
}

// TestAdapter_PerRuleEvidence verifies the FakeEncryptedPresence scenario
// fires Rule 4 evidence at honest receivers, classified under
// obftadapter.RuleFakeEncryptedPresence. Exercises the full per-rule plumbing
// path (instance.Evidence() collection, adapter ruleKey mapping, EvidenceByRule
// map propagation) AND validates that Rule 4 detection actually fires under
// the canonical chained-decrypt-fails scenario.
func TestAdapter_PerRuleEvidence(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzFakeEncryptedPresence,
		ByzOperators: []ct.OperatorID{1},
		Layer:        1,
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "FakeEncryptedPresence should fall through to L_1")

	totalRule4 := 0
	for _, oo := range out.PerOp {
		totalRule4 += oo.EvidenceByRule[obftadapter.RuleFakeEncryptedPresence]
	}
	require.GreaterOrEqual(t, totalRule4, 3,
		"all 3 honest receivers should fire Rule 4 against byz; got: %v",
		operatorEvidence(out.PerOp))
	t.Logf("Rule 4 fires across cluster: %d", totalRule4)
}

// TestAdapter_FakeEncryptedPresence_StaysSealed_WhenL0Decides exercises the
// OBFT.md §Slashing evidence / Rule 4 surface-ability limit: "evidence stays
// sealed when NR-quorum doesn't reach at all prior layers". Counterpart to
// TestAdapter_PerRuleEvidence (which exercises the positive detection path
// with byz=op1=L_0-leader silent → NR-quorum at L_0 → chain unlocks).
//
// Setup: byz=op2 (leads L_1 by default rotation, NOT L_0). Byz fakes
// encrypted-presence at L_2 via OverrideCommit. Since byz isn't the L_0
// leader, the healthy op1 broadcasts L_0's bundle and σ-quorum reaches at
// L_0. Production Instance.Resolve halts at the first σ-quorum (L_0) — chain
// decryption at L_1, L_2 never runs, so the garbage at L_2 is never observed
// as garbage. Rule 4 must NOT fire.
//
// This validates the spec's "Rule 4 is best-effort, conditional on slot
// progressing past prior layers' NR-quorum" claim as an in-suite property.
func TestAdapter_FakeEncryptedPresence_StaysSealed_WhenL0Decides(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// byz=op2 leads L_1 (op[k % N] rotation at K=N=4). Garbage at L_2 is two
	// layers deep — chain unlock requires NR-quorum at BOTH L_0 and L_1.
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzFakeEncryptedPresence,
		ByzOperators: []ct.OperatorID{2},
		Layer:        2,
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "L_0 leader honest → cluster must decide at L_0")
	require.Equal(t, 0, out.DecidedRound,
		"healthy L_0 path must hold (byz isn't L_0 leader); got L_%d", out.DecidedRound)

	totalRule4 := 0
	for _, oo := range out.PerOp {
		totalRule4 += oo.EvidenceByRule[obftadapter.RuleFakeEncryptedPresence]
	}
	require.Equal(t, 0, totalRule4,
		"Rule 4 must NOT fire when chain stays sealed (cluster decides at L_0 → no NR-quorum → no chain decryption at L_2 → garbage never observed). got evidence: %v",
		operatorEvidence(out.PerOp))

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("Rule 4 sealed: 0 fires (decided L_%d, chain at L_2 never unlocked)", out.DecidedRound)
}

// TestAdapter_OfflineAggregator_HealthyOneRecon verifies the aggregator
// records exactly one reconstruction on a healthy slot (the decided V) —
// Pigeonhole 2's load-bearing safety claim under all-honest.
func TestAdapter_OfflineAggregator_HealthyOneRecon(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.OfflineAgg.NoOfflineDoubleV,
		"healthy run must satisfy NoOfflineDoubleV: %s", out.OfflineAgg)
	require.Equalf(t, 1, len(out.OfflineAgg.Reconstructions),
		"healthy run should yield exactly one distinct V (the decided V); got: %s",
		out.OfflineAgg)
}

// TestAdapter_MaxMEVFetch_HealthyAtBoundary exercises the OBFT.md §Timing
// budget max-MEV operating point: every leader broadcasts EXACTLY at
// T_broadcast_max_k (LeaderBroadcastOffset = 0 for every layer). Per spec
// §Setting, `B_0 = 1 BTT` decomposes as "0.5 BTT typical-mesh propagation +
// 0.5 BTT convergence buffer" — so the test uses `ConstantDelay{D: BTT/2}`
// (matching P99 ≈ 150ms typical propagation in the spec's Config A) leaving
// the half-BTT convergence buffer intact. Bundle arrives at
// T_broadcast_max_0 + 0.5 BTT = T_commit − 0.5 BTT, comfortably inside the
// acceptance window. Cluster decides at L_0.
//
// Validates: (a) Protocol.MaxMEVFetch zeros the fetch buffer end-to-end,
// (b) the spec's B_k = "typical propagation + convergence buffer" decomposition
// at max-MEV fetch holds in simulation.
func TestAdapter_MaxMEVFetch_HealthyAtBoundary(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Network = ct.ConstantDelay{D: cfg.BTT / 2} // typical-mesh propagation per spec B_k decomposition

	out, err := obftadapter.Protocol{MaxMEVFetch: true}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "max-MEV op-point should decide at L_0 (typical propagation + convergence buffer)")
	require.Equal(t, 0, out.DecidedRound, "max-MEV op-point must decide at L_0 fastest path")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("MaxMEVFetch op-point: decided at %v on L_%d", out.DecisionTime, out.DecidedRound)
}

// TestAdapter_MaxMEVFetch_FallsThroughWhenConvergenceBufferConsumed exercises
// the spec's pathology: max-MEV fetch (zero broadcast offset) PLUS full-BTT
// propagation (= 1 BTT, no convergence buffer left within B_0). Per spec
// §Setting, this is the boundary where event-ordering between the L_0 arrival
// and the operator's T_commit view can flip the outcome from σ to NR. With
// the test sim's deterministic event ordering (evtPhaseTwoStart at seq N
// fires before evtPhase1Arrival at seq N+M when both land at T_commit), the
// operator commits NR at L_0 → fall-through to L_1.
//
// Validates: (a) the spec's "convergence buffer in B_k" warning is observable
// — at the exact boundary, max-MEV fetch is NOT guaranteed at L_0 under
// full-BTT propagation, (b) the K-layer fall-through correctly handles the
// boundary miss.
func TestAdapter_MaxMEVFetch_FallsThroughWhenConvergenceBufferConsumed(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// Override Network to 2·BTT propagation — under the reflood-aware schedule
	// (B_0 = 2·BTT + RefloodDelay, default RefloodDelay=0 in consensustest), this
	// consumes the entire B_0 budget with zero margin. The spec pathology
	// this test exercises: max-MEV fetch (zero broadcast offset) + propagation
	// exactly at B_0 boundary → ordering between L_0 arrival and T_commit can
	// flip the outcome from σ to NR.
	cfg.Network = ct.ConstantDelay{D: 2 * cfg.BTT}

	out, err := obftadapter.Protocol{MaxMEVFetch: true}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "should still decide via K-layer fall-through")
	require.GreaterOrEqual(t, out.DecidedRound, 1,
		"max-MEV + B_0-boundary propagation: L_0 should NOT decide (convergence buffer consumed)")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	t.Logf("MaxMEVFetch + 2·BTT propagation: fell through to L_%d at %v", out.DecidedRound, out.DecisionTime)
}

// TestAdapter_ByzWithholdLeader verifies the deepest-layer leader silenced
// pattern: at K = n = 4 with byz=op4 (the L_3 leader), the cluster decides
// at L_0 (op1 leader still broadcasts healthy) without ever reaching the
// silenced deepest layer. Validates that silencing a deeper-layer leader
// is irrelevant when shallower layers succeed — the assertion is
// DecidedRound < 3 (must NOT need L_3), which the L_0 path satisfies.
//
// For the case where ALL layers must be exhausted before the slot misses,
// see TestComparison_Matrix's all-silent scenario.
func TestAdapter_ByzWithholdLeader(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// Default rotation: L_0=op1, L_1=op2, L_2=op3, L_3=op4. byz=op4 silences L_3.
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzWithholdLeader, ByzOperators: []ct.OperatorID{4}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "should decide at a non-deepest layer")
	require.Less(t, out.DecidedRound, 3, "should NOT need the silenced deepest layer (L_3)")
}

// TestAdapter_ByzWithholdLeader_K2 verifies the deepest-layer-silenced pattern
// at K=2 (BFT-liveness minimum at f=1): rotation covers ops 1..2, so byz=op2
// is the deepest leader (L_1). L_1 silent; L_0 (op1) broadcasts healthy →
// cluster decides at L_0. Confirms K=2 is a first-class supported config for
// this byz pattern when the byz is paired with a leader in the K-truncated
// rotation.
func TestAdapter_ByzWithholdLeader_K2(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.K = 2
	// At K=2 the leader rotation is L_0=op1, L_1=op2. byz=op2 silences L_1.
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzWithholdLeader, ByzOperators: []ct.OperatorID{2}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "L_0 healthy → cluster should decide at L_0")
	require.Equal(t, 0, out.DecidedRound, "silenced deepest L_1 is irrelevant when L_0 succeeds")
}

// TestAdapter_ByzWithholdLeader_K2_ByzNotInRotation documents the no-op
// fallback case: at K=2 with byz=op4 (outside the K=2 leader rotation
// {op1, op2}), the byz isn't a leader at any layer, so the pattern doesn't
// engage and the cluster decides healthy. Pairs with the byz.go doc
// comment's guidance for K < N callers.
func TestAdapter_ByzWithholdLeader_K2_ByzNotInRotation(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.K = 2
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzWithholdLeader, ByzOperators: []ct.OperatorID{4}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided)
	require.Equal(t, 0, out.DecidedRound, "byz=op4 leads no layer at K=2; pattern is a no-op")
}

// TestAdapter_ByzCertWithholding verifies that a byz refusing cert gossip
// doesn't break the slot — honest ops reconstruct independently.
func TestAdapter_ByzCertWithholding(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzCertWithholding, ByzOperators: []ct.OperatorID{4}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "honest ops should still reconstruct independently of byz cert gossip")
	require.Equal(t, 0, out.DecidedRound, "healthy path holds at L_0")
}

// TestAdapter_ByzCrossSigning verifies Rule 1 evidence fires when byz emits
// BOTH σ AND NR at the same layer. The pattern auto-targets the byz's own
// leader layer (where silent-leader behavior produces a real NR partial); the
// adapter then injects a forged σ entry at that layer. At default rotation,
// op2 leads L_1 — so byz=op2 yields Rule 1 evidence at L_1.
func TestAdapter_ByzCrossSigning(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzCrossSigning,
		ByzOperators: []ct.OperatorID{2},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	totalRule1 := 0
	for _, oo := range out.PerOp {
		totalRule1 += oo.EvidenceByRule[obftadapter.RuleCrossSigning]
	}
	require.GreaterOrEqual(t, totalRule1, 1,
		"at least one honest op should fire Rule 1; got: %v", operatorEvidence(out.PerOp))
	t.Logf("Rule 1 fires across cluster: %d", totalRule1)
}

// TestAdapter_ByzFakePlaintextSigma verifies Rule 5 evidence fires when byz
// emits a plaintext σ at L_0 on a V no Phase-1 leader produced.
func TestAdapter_ByzFakePlaintextSigma(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzFakePlaintextSigma,
		ByzOperators: []ct.OperatorID{2},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	totalRule5 := 0
	for _, oo := range out.PerOp {
		totalRule5 += oo.EvidenceByRule[obftadapter.RuleFakePlaintextSigma]
	}
	require.GreaterOrEqual(t, totalRule5, 1,
		"at least one honest op should fire Rule 5; got: %v", operatorEvidence(out.PerOp))
	t.Logf("Rule 5 fires across cluster: %d", totalRule5)
}

// TestAdapter_LeaderEquivocation_Rule2 verifies Rule 2 evidence fires when
// the L_0 leader emits two distinct Phase-1 bundles (one V to each subset of
// honest receivers). Honest receivers retain both bundles → detect leader
// equivocation → fire Rule 2 with self-contained slashable proof. Uses
// ByzEquivocateAllNR which floods both V's to all honest, ensuring every
// honest sees both bundles.
func TestAdapter_LeaderEquivocation_Rule2(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzEquivocateAllNR,
		ByzOperators: []ct.OperatorID{1},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	totalRule2 := 0
	for _, oo := range out.PerOp {
		totalRule2 += oo.EvidenceByRule[obftadapter.RuleLeaderEquivocation]
	}
	require.GreaterOrEqual(t, totalRule2, 1,
		"at least one honest op should fire Rule 2; got: %v", operatorEvidence(out.PerOp))
	t.Logf("Rule 2 fires across cluster: %d", totalRule2)
}

// TestAdapter_ByzCrossOnionEquivocation verifies Rule 3 per-layer evidence
// fires when byz emits two distinct Commits with different σ at the same layer.
func TestAdapter_ByzCrossOnionEquivocation(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzCrossOnionEquivocation,
		ByzOperators: []ct.OperatorID{2},
		Layer:        0,
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	totalRule3 := 0
	for _, oo := range out.PerOp {
		// Rule 3 has two variants: top-level (Layer=-1) and per-layer.
		totalRule3 += oo.EvidenceByRule[obftadapter.RuleCrossOnionEquivocation]
		totalRule3 += oo.EvidenceByRule[obftadapter.RuleCommitEquivocation]
	}
	require.GreaterOrEqual(t, totalRule3, 1,
		"at least one honest op should fire Rule 3; got: %v", operatorEvidence(out.PerOp))
	t.Logf("Rule 3 fires across cluster: %d", totalRule3)
}

// TestAdapter_ByzLateLeaderBroadcast verifies the spec's Class A asymmetric-
// propagation claim: when L_0 leader broadcasts so late that the bundle's
// first-observation lands past T_commit at every honest receiver, the
// cluster falls through to L_1 (whose leader broadcasts on time). Validates
// the per-layer absorption-window mechanism.
func TestAdapter_ByzLateLeaderBroadcast(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// byz=op1 is the L_0 leader by default rotation.
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzLateLeaderBroadcast, ByzOperators: []ct.OperatorID{1}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "cluster should fall through to L_1 (honest leader)")
	require.GreaterOrEqual(t, out.DecidedRound, 1,
		"should NOT decide at L_0 (byz bundle past T_commit); got L_%d", out.DecidedRound)

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("Late-L_0-broadcast: decided at %v on L_%d", out.DecisionTime, out.DecidedRound)
}

// TestAdapter_ByzAggregatorBypass_TriggersSafetyDetection is a negative
// test: the byz forges commits claiming distinct identities and a different
// V at L_0. The OfflineAggregator's worst-case-byz-visibility model
// reconstructs both V signatures (the canonical V from honest σ-quorum AND
// the forged V_prime from byz's forged-identity σ partials). NoOfflineDoubleV
// must fire — validates the safety machinery actually detects this class
// of attack.
//
// Calls Run() directly (not RunScenarioOnProtocol) because the safety check
// in RunScenarioOnProtocol panics on NoOfflineDoubleV violations; this test
// inspects ComputeSafetyReport's verdict explicitly.
//
// Tests both byz placements: byz=L_0 leader (op1) and byz=non-leader (op2).
// Both must trigger detection; the bypass forges from all-other-than-self
// to ensure ≥ qV partials on V_prime regardless of byz position.
func TestAdapter_ByzAggregatorBypass_TriggersSafetyDetection(t *testing.T) {
	for _, byzOp := range []ct.OperatorID{1, 2} {
		t.Run(fmt.Sprintf("byz=op%d", byzOp), func(t *testing.T) {
			cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
			cfg.Byz = ct.ByzPattern{Kind: ct.ByzAggregatorBypass, ByzOperators: []ct.OperatorID{byzOp}}
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err)

			rep := ct.ComputeSafetyReport(out)
			require.Falsef(t, rep.NoOfflineDoubleV,
				"byz=op%d: aggregator bypass MUST trigger NoOfflineDoubleV; got: %s", byzOp, rep)
			require.GreaterOrEqualf(t, len(out.OfflineAgg.Reconstructions), 2,
				"byz=op%d: aggregator should reconstruct ≥ 2 distinct V signatures; got: %s",
				byzOp, out.OfflineAgg)
			t.Logf("byz=op%d AggregatorBypass: %s", byzOp, out.OfflineAgg)
		})
	}
}

// TestAdapter_PartialEquivocation_NaturalRecovery verifies the OBFT.md:443
// natural-recovery path: byz leader equivocates 2-1 (V_a → 2 honest, V_b → 1
// honest); σ-pool on V_a = 2 honest σ + leader's σ_L^V(V_a) = 3 = qV at f=1,
// n=4. Slot SUCCEEDS at L_0 with V_a despite equivocation.
//
// Per spec §Phase 2 wire format, Witnesses ship value_root + σ_V (no full V).
// The V_b recipient (op4) cannot use the witnessed σ_L^V(V_a) — it would need
// the V_a bytes which it didn't receive — so op4's σ-pool view at L_0 has
// only V_b partials and op4 falls through. Op2/op3 reach σ-quorum on V_a at
// L_0 and decide; op4 catches up via KindCertificate gossip.
//
// Rule 2 evidence does NOT fire in this scenario: each receiver only sees
// one V via Phase 1, and witnesses don't carry V (only value_root + σ_V).
// This is the deliberate spec trade-off — dropping full V from witnesses
// loses cross-receiver Rule 2 attribution in natural-recovery scenarios.
// Distinct from EquivocateSigmaLockedSplit (1-1-NR slot-miss at OBFT.md:452)
// which has only ≤ 2 partials on each V and therefore reaches no qV.
func TestAdapter_PartialEquivocation_NaturalRecovery(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzPartialEquivocation,
		ByzOperators: []ct.OperatorID{1},
		Recipients:   []ct.OperatorID{2, 3, 4}, // V_a → op2, op3; V_b → op4
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "byz fumbled equivocation: σ-pool on V_a should reach qV naturally")
	require.Equal(t, 0, out.DecidedRound, "should decide at L_0 fastest path with V_a")
	require.Equal(t, "byz-V-A", string(out.DecidedValue),
		"all honest should resolve on V_a (the majority side). got=%q", string(out.DecidedValue))

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "Pigeonhole 2: at most one V per layer cluster-wide; SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)

	t.Logf("PartialEquivocation 2-1: decided at L_%d with V=%q",
		out.DecidedRound, string(out.DecidedValue))
}

// TestAdapter_LateCommitArrival_ReResolve exercises the spec §Phase 3
// "Re-running on late KindCommit arrivals" recovery path via the NR-quorum
// late-unlock variant ("a late NR partial pushes NR-pool past qEnc at a
// layer that previously had NR-pool short of qEnc → derive the layer-k
// decryption key, unlock chained decryption for layer k+1's σ partials,
// advance the walk past k"). Validates the 1.3 framework
// (EnableLateCommitRerun + evtResolveRerun) salvages a slot that would
// otherwise miss for lack of NR-quorum to unlock chained decryption.
//
// Setup at f=1, n=4, default leader rotation (op_k leads L_{k-1}):
//   - All 3 non-leader hosts are NV at L_0 (op2, op3, op4); ops still
//     σ-emit at L_1+ (host-NV is layer-0-scoped).
//   - op4 is BYZ "delayed commit": its KindCommit at Phase 2 carries an
//     on-protocol NR partial at L_0 plus σ at L_1+, but is dispatched
//     with OverrideOwnCommitDispatchDelay = 1.5·BTT → arrives ~50ms past
//     RoundEndOffset.
//
// Cluster state:
//   - σ at L_0 (cluster-wide): op1's Phase-1 σ_L^V only = 1 < qV=3.
//   - NR at L_0 (cluster-wide): {op2, op3, op4}; with op4 delayed,
//     receivers see only {op2, op3} = 2 < qEnc=3 by RoundEndOffset.
//   - Chain at L_0 stays sealed → L_1 onion entries (where every op
//     σ-emits on V_1) are undecodable.
//
// Initial Resolve fails at L_0 (σ < qV, NR < qEnc). After op4's late
// commit arrives: NR-pool = {op2, op3, op4} = 3 = qEnc → chain key
// for L_0 derived → L_1 onion entries decoded → σ-pool at L_1 reaches
// qV → decide at L_1 via fall-through.
//
// Note: op4 (byz) self-observes its own NR partial in BuildOwnCommit, so
// op4's local state has NR-quorum at RoundEndOffset (own + op2 + op3 = 3).
// op4 decides locally at L_1 in initial Resolve. Other receivers depend on
// either (a) the rerun path after op4's late commit, or (b) cert gossip
// from op4. With EnableLateCommitRerun on, the rerun fires first; cert
// gossip from op4 still arrives but op2/op3/op1 are already decided.
func TestAdapter_LateCommitArrival_ReResolve(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Host = ct.HostInvalidForOperators{
		Layer:     0,
		Operators: map[ct.OperatorID]bool{2: true, 3: true, 4: true},
	}
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzDelayedCommit,
		ByzOperators: []ct.OperatorID{4},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "late-NR re-resolve must salvage the slot")
	// Outcome.DecidedRound is the EARLIEST cluster-wide decision time +
	// layer; that's op4's local decide at L_1 (RoundEndOffset). Receivers
	// rescued via rerun/cert decide later at the same L_1. Both fine — we
	// care that the cluster decides, which validates the recovery path.
	require.Equal(t, 1, out.DecidedRound,
		"cluster should fall through to L_1 via NR-quorum (incl. late op4 NR)")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)

	// Non-byz receivers decide via the rerun path when op4's late commit
	// arrives (~T_commit + BTT + 1.5·BTT = 3900ms at BTT=200). If a
	// regression broke the rerun path, the cluster would rescue via op4's
	// cert-gossip path instead, which arrives strictly later (~4050ms =
	// op4's local Resolve at RoundEndOffset + 1·BTT cert propagation).
	// Assert decide time is BELOW the cert-gossip floor so a regression
	// surfaces as a test failure rather than coinciding with a slower
	// salvage path.
	const rerunPathArrival = 3950 * time.Millisecond
	for _, op := range []ct.OperatorID{1, 2, 3} {
		oo, ok := out.PerOp[op]
		require.True(t, ok, "op%d missing from PerOp", op)
		require.True(t, oo.Decided, "op%d should decide", op)
		require.LessOrEqualf(t, oo.Time, rerunPathArrival,
			"op%d must decide via the rerun path (< ~3950ms), not via the slower cert-gossip path (~4050ms); got %v",
			op, oo.Time)
	}
	t.Logf("Late-NR re-resolve: cluster decided at %v on L_%d; per-op times: op1=%v op2=%v op3=%v op4=%v",
		out.DecisionTime, out.DecidedRound,
		out.PerOp[1].Time, out.PerOp[2].Time, out.PerOp[3].Time, out.PerOp[4].Time)
}

// TestAdapter_ByzWitnessForgery_TriggersSafetyDetection is the sibling
// negative test to ByzAggregatorBypass: it exercises recordCommitToAggregator's
// Witnesses[] path. Byz emits an extra commit whose Witnesses[] credit ≥ qV
// honest leaders with σ partials on a V_prime at L_1; combined with honest
// σ-quorum on the canonical V at L_0, the OfflineAggregator must report
// NoOfflineDoubleV=false.
//
// Without this test, a regression to the Witnesses crediting at
// obft/events.go's recordCommitToAggregator (the only call site of
// ObserveSigma keyed on w.Leader) would slip past every other test.
//
// Calls Run() directly (not RunScenarioOnProtocol) because the safety check
// in RunScenarioOnProtocol panics on NoOfflineDoubleV violations; this test
// inspects ComputeSafetyReport's verdict explicitly.
func TestAdapter_ByzWitnessForgery_TriggersSafetyDetection(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzWitnessForgery, ByzOperators: []ct.OperatorID{2}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	rep := ct.ComputeSafetyReport(out)
	require.False(t, rep.NoOfflineDoubleV,
		"witness forgery MUST trigger NoOfflineDoubleV (Witnesses path); got: %s", rep)
	require.GreaterOrEqual(t, len(out.OfflineAgg.Reconstructions), 2,
		"aggregator should reconstruct ≥ 2 distinct V signatures (canonical V at L_0 + V_prime at L_1 via Witnesses); got: %s",
		out.OfflineAgg)
	t.Logf("WitnessForgery: %s", out.OfflineAgg)
}

func clusterName(n int) string { return fmt.Sprintf("n=%d", n) }

func operatorEvidence(perOp map[ct.OperatorID]ct.OperatorOutcome) map[ct.OperatorID]map[string]int {
	out := make(map[ct.OperatorID]map[string]int, len(perOp))
	for op, oo := range perOp {
		if len(oo.EvidenceByRule) > 0 {
			out[op] = oo.EvidenceByRule
		}
	}
	return out
}
