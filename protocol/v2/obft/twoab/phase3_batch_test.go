package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the σ-walk batch-verify in twoab (audit finding F4). Mirrors
// obft/base/phase3_batch_test.go. See docs/OBFT-F4-IMPLEMENTATION-PLAN.md
// for the safety contract; the short form is "batch returning true is at
// the same security level as N successful VerifyPartial calls, and batch
// returning false MUST fall back to per-tuple verify to preserve Rule-4
// attribution per (op, layer)".
//
// twoab's σ-walk batches across three peer-message stores (peerValueMsg,
// peerNoValueMsg, peerCommit-NRDirect) per aggregatePeerLayerEntries; the
// classifySigmaFromEntries helper pushes cache-miss tuples into a shared
// pending slice that the batch verifies after all three loops complete.
// These tests exercise the F4 helpers directly; end-to-end correctness is
// covered by the existing TwoabHealthy_n4 / consensustest stress suites.

// makeStubPendingFor builds a pendingVerify for op signing v under the sim's
// share-bytes convention (the stub uses []byte{byte(op)} as both share and
// pubKeyShare).
func makeStubPendingFor(t *testing.T, s *sim, receiver *Instance, op OperatorID, v []byte) pendingVerify {
	t.Helper()
	share := []byte{byte(op)}
	signer := NewStubSigner(s.cfg.QV(), share)
	partial, err := signer.SignPartial(v)
	require.NoError(t, err)
	return pendingVerify{
		op:         op,
		pubShare:   receiver.pubKeyShares[op],
		value:      v,
		partial:    partial,
		ciphertext: []byte("ct-for-evidence"), // only consumed on fallback
	}
}

// TestTwoab_F4_BatchVerifyAndPopulate_AllPass_PopulatesCacheAndGroups —
// happy path: every tuple verifies, F1 cache populates, each tuple lands
// in groups[V_root]. Mirror of base's same-named test.
func TestTwoab_F4_BatchVerifyAndPopulate_AllPass_PopulatesCacheAndGroups(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_1_batch_happy")
	const layer = 1

	pending := []pendingVerify{
		makeStubPendingFor(t, s, receiver, 1, v),
		makeStubPendingFor(t, s, receiver, 2, v),
		makeStubPendingFor(t, s, receiver, 4, v),
	}
	for _, pv := range pending {
		require.False(t, receiver.alreadyVerified(pv.op, layer, v, pv.partial),
			"cache must be empty pre-batch")
	}

	groups := make(map[[32]byte]*sigGroup)
	require.True(t, receiver.batchVerifyAndPopulate(layer, pending, groups),
		"happy batch must return true")

	for _, pv := range pending {
		require.True(t, receiver.alreadyVerified(pv.op, layer, v, pv.partial),
			"each batched tuple must be cache-populated on batch success")
	}
	require.Len(t, groups, 1, "all partials on the same V → one sigGroup keyed by V_root")
	for _, g := range groups {
		require.Len(t, g.partials, len(pending),
			"every batched tuple must contribute to the group")
	}
}

// TestTwoab_F4_BatchVerifyAndPopulate_BatchFails_NoMutation — when one
// partial is forged, batch returns false AND leaves cache + groups
// untouched. Caller runs the sequential fallback to attribute Rule-4.
//
// Load-bearing F1-safety property — a failed batch must not populate the
// cache for the good tuples either, otherwise a future Resolve walk would
// mark a phantom good partial via cache-hit and bypass attribution.
func TestTwoab_F4_BatchVerifyAndPopulate_BatchFails_NoMutation(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_1_batch_failure")
	const layer = 1

	pending := []pendingVerify{
		makeStubPendingFor(t, s, receiver, 1, v),
		makeStubPendingFor(t, s, receiver, 2, v),
		makeStubPendingFor(t, s, receiver, 4, v),
	}
	tampered := append([]byte{}, pending[1].partial...)
	tampered[len(tampered)-1] ^= 0xFF
	pending[1].partial = tampered

	groups := make(map[[32]byte]*sigGroup)
	require.False(t, receiver.batchVerifyAndPopulate(layer, pending, groups),
		"batch with one bad sig must return false")

	for _, pv := range pending {
		require.False(t, receiver.alreadyVerified(pv.op, layer, v, pv.partial),
			"failed batch MUST NOT populate the cache for any tuple — incl. good ones")
	}
	require.Empty(t, groups, "failed batch MUST NOT add to groups")
}

// TestTwoab_F4_SequentialVerifyAndAttribute_GoodAndBadMix — fallback after
// batch failure: good tuples cache-populated + grouped; bad tuples at L_k>0
// fire Rule-4 with the EvidenceFakeEncryptedPresence shape; recordRule4
// dedupes per (op, layer). Mirror of base's same-named test.
func TestTwoab_F4_SequentialVerifyAndAttribute_GoodAndBadMix(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_1_fallback")
	const layer = 1

	good1 := makeStubPendingFor(t, s, receiver, 1, v)
	bad2 := makeStubPendingFor(t, s, receiver, 2, v)
	good4 := makeStubPendingFor(t, s, receiver, 4, v)
	tampered := append([]byte{}, bad2.partial...)
	tampered[0] ^= 0xFF
	bad2.partial = tampered

	pending := []pendingVerify{good1, bad2, good4}
	groups := make(map[[32]byte]*sigGroup)
	receiver.sequentialVerifyAndAttribute(layer, pending, groups)

	require.True(t, receiver.alreadyVerified(good1.op, layer, v, good1.partial),
		"good tuple must be cache-populated")
	require.True(t, receiver.alreadyVerified(good4.op, layer, v, good4.partial),
		"good tuple must be cache-populated")
	require.Len(t, groups, 1, "good partials on same V → one sigGroup")
	for _, g := range groups {
		require.Len(t, g.partials, 2, "two good tuples in the group")
	}

	require.False(t, receiver.alreadyVerified(bad2.op, layer, v, bad2.partial),
		"bad tuple MUST NOT be cache-populated")

	ev := receiver.Evidence()
	require.Len(t, ev, 1, "exactly one Rule-4 evidence for the one bad tuple at L_k>0")
	require.Equal(t, EvidenceFakeEncryptedPresence, ev[0].Rule)
	require.Equal(t, bad2.op, ev[0].OperatorID)
	require.Equal(t, layer, ev[0].Layer)
	require.NotNil(t, ev[0].FakeEncryptedPresence)
	require.Equal(t, bad2.ciphertext, ev[0].FakeEncryptedPresence.Ciphertext,
		"evidence must carry the original ciphertext")
	require.Equal(t, []byte(bad2.partial), ev[0].FakeEncryptedPresence.DecryptedBytes,
		"evidence must carry the decrypted (failing) bytes")
}

// TestTwoab_F4_SequentialVerifyAndAttribute_L0_NoRule4 — at L_0 a failing
// tuple in the fallback does NOT fire Rule-4 (Rule 5 is the L_0 attribution
// rule and fires at observe time). In practice the L_0 fallback is
// unreachable because L_0 cache hits cover all observed entries; the
// guard is defense-in-depth.
func TestTwoab_F4_SequentialVerifyAndAttribute_L0_NoRule4(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_0_no_rule4")

	bad := makeStubPendingFor(t, s, receiver, 2, v)
	tampered := append([]byte{}, bad.partial...)
	tampered[0] ^= 0xFF
	bad.partial = tampered

	groups := make(map[[32]byte]*sigGroup)
	receiver.sequentialVerifyAndAttribute(0, []pendingVerify{bad}, groups)

	require.Empty(t, groups, "bad tuple at L_0 must not enter group")
	require.False(t, receiver.alreadyVerified(bad.op, 0, v, bad.partial),
		"bad tuple at L_0 must not be cache-populated")
	require.Empty(t, receiver.Evidence(),
		"L_0 fallback MUST NOT fire Rule-4 evidence (Rule 5 fires at observe time)")
}

// TestTwoab_F4_SequentialVerifyAndAttribute_DedupPerOpLayer — recordRule4
// dedupes per (op, layer); two bad tuples at same key produce one evidence.
func TestTwoab_F4_SequentialVerifyAndAttribute_DedupPerOpLayer(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_1_dedup")
	const layer = 1

	bad1 := makeStubPendingFor(t, s, receiver, 2, v)
	bad2 := makeStubPendingFor(t, s, receiver, 2, v)
	t1 := append([]byte{}, bad1.partial...)
	t1[0] ^= 0xFF
	bad1.partial = t1
	t2 := append([]byte{}, bad2.partial...)
	t2[1] ^= 0xFF
	bad2.partial = t2

	groups := make(map[[32]byte]*sigGroup)
	receiver.sequentialVerifyAndAttribute(layer, []pendingVerify{bad1, bad2}, groups)
	require.Len(t, receiver.Evidence(), 1,
		"recordRule4 dedupes per (op, layer); only one evidence for two distinct bad tuples")
}
