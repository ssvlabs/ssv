package twoab

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// classifyTwoabMiss is unreachable from the external test package and is
// otherwise only covered via Run-level integration tests. These internal
// branches exercise each classifier regime in isolation so a future change
// to one branch can't quietly break the others — in particular the
// deadlock-walk split into the recoverable propagation stall (undelivered)
// vs. the genuine validity / σ-split wedges. Mirrors the regimes documented
// above classifyTwoabMiss.
func TestClassifyTwoabMiss_Branches(t *testing.T) {
	const deadline = 3900 * time.Millisecond
	cases := []struct {
		name          string
		preDecided    bool
		preRound      int
		preTime       time.Duration
		deadlockLayer int
		kind          deadlockKind
		want          string
	}{
		{
			name:          "decided_clipped_layer_0",
			preDecided:    true,
			preRound:      0,
			preTime:       4100 * time.Millisecond,
			deadlockLayer: -1,
			kind:          deadlockNone,
			want:          "Cluster ready to submit at layer 0, past the submit deadline",
		},
		{
			name:          "stall_undelivered_layer_0",
			preDecided:    false,
			preRound:      -1,
			deadlockLayer: 0,
			kind:          deadlockUndelivered,
			want:          "Cluster stalled at layer 0 — value didn't reach σ-quorum in time (undelivered)",
		},
		{
			name:          "deadlock_validity_layer_0",
			preDecided:    false,
			preRound:      -1,
			deadlockLayer: 0,
			kind:          deadlockValidity,
			want:          "Cluster deadlocked at layer 0 (validity split — σ impossible for the dissenting cohort, NR-default gated)",
		},
		{
			name:          "deadlock_split_layer_1",
			preDecided:    false,
			preRound:      -1,
			deadlockLayer: 1,
			kind:          deadlockSplit,
			want:          "Cluster deadlocked at layer 1 (σ split across values, none reaching qV; NR-default gated)",
		},
		{
			name:          "exhausted_no_deadlock",
			preDecided:    false,
			preRound:      -1,
			deadlockLayer: -1,
			kind:          deadlockNone,
			want:          "Cluster never assembled a threshold signature at any layer",
		},
		// Decided-and-clipped outranks a stray deadlock signal: if any op
		// decided locally past the deadline, that's the cluster outcome.
		{
			name:          "decided_clipped_outranks_deadlock",
			preDecided:    true,
			preRound:      0,
			preTime:       4500 * time.Millisecond,
			deadlockLayer: 0,
			kind:          deadlockUndelivered,
			want:          "Cluster ready to submit at layer 0, past the submit deadline",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyTwoabMiss(tc.preDecided, tc.preRound, tc.preTime, deadline, tc.deadlockLayer, tc.kind)
			require.Equal(t, tc.want, got)
		})
	}
}

// classifyDeadlockKind is the evidence-based decision table behind the
// deadlock split. The load-bearing case is the all-honest single-value
// deadlock — no host-rejection, exactly one proposed value — which must
// resolve to the recoverable stall (deadlockUndelivered), NOT σ-split: an
// all-honest single-leader cluster cannot produce a genuine value split, so
// the Healthy panel must never show "σ split across values".
func TestClassifyDeadlockKind(t *testing.T) {
	cases := []struct {
		name           string
		hostRejected   bool
		distinctValues int
		want           deadlockKind
	}{
		{"all_honest_single_value", false, 1, deadlockUndelivered},
		{"nothing_retained", false, 0, deadlockUndelivered},
		{"genuine_split_two_values", false, 2, deadlockSplit},
		{"genuine_split_three_values", false, 3, deadlockSplit},
		{"host_rejected_single_value", true, 1, deadlockValidity},
		{"host_rejected_outranks_split", true, 2, deadlockValidity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, classifyDeadlockKind(tc.hostRejected, tc.distinctValues))
		})
	}
}

// TestRecorder_LayerEntryKeyedByV_NotPayload is the regression test for
// the bucket-1 fix (commit 834e4d10e): twoab/events.go's
// recordValueMsgToAggregator / recordNoValueMsgToAggregator /
// recordCommitToAggregator were passing e.Payload (encrypted ciphertext,
// differs per emitter under chained-IBE) to ObserveEncryptedClaim,
// scattering contributions across distinct buckets and rendering
// NoOfflineDoubleV at 2abOBFT L_k>0 vacuous. The fix passes e.V
// (plaintext) so the cluster-wide aggregator counts cluster-wide
// distinct emitters on the same V.
//
// This test catches a regression where someone changes back to
// e.Payload: it constructs LayerEntries with V_plaintext set AND a
// distinct Payload, then asserts the recorder keys buckets by
// sha256(V_plaintext) — NOT sha256(Payload). All three recorder
// functions are exercised because each independently consumes the V
// field.
func TestRecorder_LayerEntryKeyedByV_NotPayload(t *testing.T) {
	const layer = 1 // L_k>0; EncryptedClaims path
	emitter := twoab.OperatorID(2)
	v := twoab.Value("V_plaintext_at_L1")
	payload := []byte("ciphertext_garbage_differs_per_emitter")
	vRoot := sha256.Sum256(v)
	payloadRoot := sha256.Sum256(payload)
	require.NotEqual(t, vRoot, payloadRoot,
		"test invariant: V plaintext and Payload ciphertext must hash distinctly")

	t.Run("recordValueMsgToAggregator", func(t *testing.T) {
		agg := ct.NewOfflineAggregator(4)
		vm := &twoab.ValueMsg{
			OperatorID: emitter,
			V:          twoab.Value("V_at_L0"),
			ValueRoot:  sha256.Sum256([]byte("V_at_L0")),
			L0Partial:  twoab.Signature{0x01},
			LayerEntries: []twoab.LayerEntry{
				{Layer: layer, Kind: twoab.LayerEntrySigmaChained, V: v, Payload: payload},
			},
		}
		recordValueMsgToAggregator(agg, emitter, vm)
		assertSigmaBucketedByV(t, agg, emitter, layer, vRoot, payloadRoot)
	})

	t.Run("recordNoValueMsgToAggregator", func(t *testing.T) {
		agg := ct.NewOfflineAggregator(4)
		nv := &twoab.NoValueMsg{
			OperatorID: emitter,
			LayerEntries: []twoab.LayerEntry{
				{Layer: layer, Kind: twoab.LayerEntrySigmaChained, V: v, Payload: payload},
			},
		}
		recordNoValueMsgToAggregator(agg, emitter, nv)
		assertSigmaBucketedByV(t, agg, emitter, layer, vRoot, payloadRoot)
	})

	t.Run("recordCommitToAggregator_NRDirect", func(t *testing.T) {
		agg := ct.NewOfflineAggregator(4)
		c := &twoab.Commit{
			OperatorID: emitter,
			Side:       twoab.CommitSideNRDirect,
			LayerEntries: []twoab.LayerEntry{
				{Layer: layer, Kind: twoab.LayerEntrySigmaChained, V: v, Payload: payload},
			},
		}
		recordCommitToAggregator(agg, emitter, c)
		assertSigmaBucketedByV(t, agg, emitter, layer, vRoot, payloadRoot)
	})
}

// assertSigmaBucketedByV verifies the post-recorder aggregator state
// keys the SigmaChained entry by V's hash (claimed-sender path via
// EncryptedClaims; by-emitter path via SigmaByEmitter), NOT by
// Payload's hash. Fires loudly if someone reverts the bucket-1 fix.
func assertSigmaBucketedByV(t *testing.T, agg *ct.OfflineAggregator, emitter twoab.OperatorID, layer int, vRoot, payloadRoot [32]byte) {
	t.Helper()
	em := ct.OperatorID(emitter)
	// Claimed-sender path: EncryptedClaims[(layer, vRoot)] must have the
	// emitter. EncryptedClaims[(layer, payloadRoot)] MUST be empty.
	vKey := ct.SigmaKey{Layer: layer, ValueHash: vRoot}
	payloadKey := ct.SigmaKey{Layer: layer, ValueHash: payloadRoot}
	require.Contains(t, agg.EncryptedClaims, vKey,
		"EncryptedClaims must contain a bucket keyed by sha256(V); regression: someone reverted e.V → e.Payload")
	require.Contains(t, agg.EncryptedClaims[vKey], em,
		"EncryptedClaims[V_root] must record the emitter")
	require.NotContains(t, agg.EncryptedClaims, payloadKey,
		"EncryptedClaims must NOT contain a bucket keyed by sha256(Payload); regression: someone reverted to e.Payload")

	// By-emitter path: SigmaByEmitter[(emitter, layer, vRoot)] must be
	// present; SigmaByEmitter[(emitter, layer, payloadRoot)] absent.
	vEmitterKey := ct.ByEmitterSigmaKey{Emitter: em, Layer: layer, ValueHash: vRoot}
	payloadEmitterKey := ct.ByEmitterSigmaKey{Emitter: em, Layer: layer, ValueHash: payloadRoot}
	require.Contains(t, agg.SigmaByEmitter, vEmitterKey,
		"SigmaByEmitter must record (emitter, layer, sha256(V))")
	require.NotContains(t, agg.SigmaByEmitter, payloadEmitterKey,
		"SigmaByEmitter must NOT key on sha256(Payload)")
}

// TestRecorder_B3SubsumedByB1_LeaderCrossSigns is the integration test
// covering the bucket-2 claim "HonestCrossPhaseExclusive subsumes B3
// (leader's Phase-1 σ_V counts toward σ-side, so a leader who NR/NV's
// their own layer triggers the same collision)" — specifically for
// 2abOBFT's split recording paths: σ_V flows through
// recordValueMsgToAggregator's vm.L0Partial branch, while NR flows
// through recordCommitToAggregator's CommitSideNR branch. Both must
// record under the same emitter identity for the B1 check at L_0 to
// fire when a (hypothetically buggy) leader emits both.
//
// Synthesizes the recording side directly: the protocol-side σ-XOR-NR
// gate (transitionToSigma / transitionToNR in obft/twoab.Instance)
// would prevent this in correct code; this test exercises only the
// recorders + the safety check, simulating the EKM-bypass regression
// that B1 is meant to catch.
func TestRecorder_B3SubsumedByB1_LeaderCrossSigns(t *testing.T) {
	leader := twoab.OperatorID(1)
	v := twoab.Value("V_at_L0")
	agg := ct.NewOfflineAggregator(4)

	// 1. Leader emits ValueMsg with L0Partial — records σ at L_0 for
	//    leader via ObserveSigma + ObserveSigmaByEmitter.
	vm := &twoab.ValueMsg{
		OperatorID: leader,
		V:          v,
		ValueRoot:  sha256.Sum256(v),
		L0Partial:  twoab.Signature{0x01},
	}
	recordValueMsgToAggregator(agg, leader, vm)

	// 2. Same leader emits a Commit Side=NR — records NR at L_0 for
	//    leader via ObserveNR + ObserveNRByEmitter. Under correct
	//    protocol this is impossible (σ-XOR-NR EKM gate); the test
	//    simulates the regression where the gate breaks.
	c := &twoab.Commit{
		OperatorID: leader,
		Side:       twoab.CommitSideNR,
	}
	recordCommitToAggregator(agg, leader, c)

	// 3. Assert both maps record the leader at L_0.
	em := ct.OperatorID(leader)
	sigKey := ct.ByEmitterSigmaKey{Emitter: em, Layer: 0, ValueHash: sha256.Sum256(v)}
	nrKey := ct.ByEmitterNRKey{Emitter: em, Layer: 0}
	require.Contains(t, agg.SigmaByEmitter, sigKey,
		"leader's L0Partial must record under SigmaByEmitter[leader, L_0, V]")
	require.Contains(t, agg.NRByEmitter, nrKey,
		"leader's Commit Side=NR must record under NRByEmitter[leader, L_0]")

	// 4. Run the offline-aggregator report + ComputeSafetyReport and
	//    assert HonestCrossPhaseExclusive fires. The check filters
	//    byzantine ops via Outcome.Byz; with empty byz set, leader is
	//    treated as honest → the σ+NR collision flags B1.
	report := agg.AttemptAll()
	out := ct.Outcome{
		Decided:      true,
		DecidedValue: []byte(v),
		DecidedRound: 0,
		PerOp: map[ct.OperatorID]ct.OperatorOutcome{
			em: {Decided: true, Round: 0, Value: []byte(v)},
		},
		OfflineAgg: report,
		// Byz: zero-value → leader treated as honest; B1 check fires.
	}
	r := ct.ComputeSafetyReport(out)
	require.False(t, r.HonestCrossPhaseExclusive,
		"leader σ_V + NR at L_0 must trigger B1 (subsumes B3)")
	require.Len(t, r.CrossPhaseEvidence, 1, "evidence must name the offending op + layer")
	require.Equal(t, em, r.CrossPhaseEvidence[0].Operator)
	require.Equal(t, 0, r.CrossPhaseEvidence[0].Layer)
}
