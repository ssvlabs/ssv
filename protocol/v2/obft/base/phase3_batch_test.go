package base

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the σ-walk batch-verify (audit finding F4). The safety
// contract in short form is "batch returning true is at the same security level as N
// successful VerifyPartial calls, and batch returning false MUST fall back
// to per-tuple verify to preserve Rule-4 attribution per (op, layer)".
//
// These tests exercise the F4 helpers (batchVerifyAndPopulate +
// sequentialVerifyAndAttribute) directly. End-to-end via Resolve at
// L_k>0 is exercised by the existing consensustest stress suite and the
// runner-layer integration tests, all of which pass with the F4 wiring
// in place.

// makeStubPendingFor builds a pendingVerify for op signing v under the sim's
// share-bytes convention (the stub uses []byte{byte(op)} as both share and
// pubKeyShare; see newSim's pubKeyShares assignment). The receiver's
// pubKeyShares[op] is what the σ-walk looks up at verify time.
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

// TestObft_F4_BatchVerifyAndPopulate_AllPass_PopulatesCacheAndGroups —
// happy path: every tuple in the batch verifies, cache gets populated for
// every (op, layer, value, partial), and each tuple appears in the
// resulting sigGroup. This is the σ-walk's L_k>0 first-walk fast path.
func TestObft_F4_BatchVerifyAndPopulate_AllPass_PopulatesCacheAndGroups(t *testing.T) {
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

	var groups []*sigGroup
	require.True(t, receiver.batchVerifyAndPopulate(layer, pending, &groups),
		"happy batch must return true")

	for _, pv := range pending {
		require.True(t, receiver.alreadyVerified(pv.op, layer, v, pv.partial),
			"each batched tuple must be cache-populated on batch success")
	}
	require.Len(t, groups, 1, "all partials on the same V → one sigGroup")
	require.Len(t, groups[0].partials, len(pending),
		"every batched tuple must contribute to the group")
}

// TestObft_F4_BatchVerifyAndPopulate_BatchFails_NoMutation — when one
// partial in the batch is forged, the batch returns false AND leaves the
// cache + groups completely untouched. The caller (tryReconstructLayer)
// runs the sequential fallback to attribute Rule-4 evidence.
//
// This is the load-bearing F1-safety property: a failed batch must not
// populate the cache for ANY tuple (even the good ones in the same batch),
// otherwise a future Resolve walk would mark a phantom good partial via
// cache-hit and bypass the sequential fallback's attribution logic.
func TestObft_F4_BatchVerifyAndPopulate_BatchFails_NoMutation(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_1_batch_failure")
	const layer = 1

	pending := []pendingVerify{
		makeStubPendingFor(t, s, receiver, 1, v),
		makeStubPendingFor(t, s, receiver, 2, v),
		makeStubPendingFor(t, s, receiver, 4, v),
	}
	// Tamper with op2's partial — flips last byte; the stub's byte-compare
	// verify rejects it.
	tampered := append([]byte{}, pending[1].partial...)
	tampered[len(tampered)-1] ^= 0xFF
	pending[1].partial = tampered

	var groups []*sigGroup
	require.False(t, receiver.batchVerifyAndPopulate(layer, pending, &groups),
		"batch with one bad sig must return false")

	for _, pv := range pending {
		require.False(t, receiver.alreadyVerified(pv.op, layer, v, pv.partial),
			"failed batch MUST NOT populate the cache for any tuple — incl. good ones")
	}
	require.Empty(t, groups, "failed batch MUST NOT add to groups")
}

// TestObft_F4_SequentialVerifyAndAttribute_GoodAndBadMix — the fallback
// run after a batch-failure: each good tuple is cache-populated + added to
// groups; each bad tuple at L_k>0 fires Rule-4 evidence with the original
// (op, layer, ciphertext, decryptedBytes) shape; recordRule4 dedup applies
// (per-(op, layer)).
//
// This preserves the pre-F4 per-sig attribution exactly.
func TestObft_F4_SequentialVerifyAndAttribute_GoodAndBadMix(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_2_fallback")
	const layer = 2

	good1 := makeStubPendingFor(t, s, receiver, 1, v)
	bad2 := makeStubPendingFor(t, s, receiver, 2, v)
	good4 := makeStubPendingFor(t, s, receiver, 4, v)
	// Corrupt bad2's partial.
	tampered := append([]byte{}, bad2.partial...)
	tampered[0] ^= 0xFF
	bad2.partial = tampered

	pending := []pendingVerify{good1, bad2, good4}
	var groups []*sigGroup
	receiver.sequentialVerifyAndAttribute(layer, pending, &groups)

	// Good tuples populated + added to group.
	require.True(t, receiver.alreadyVerified(good1.op, layer, v, good1.partial),
		"good tuple must be cache-populated")
	require.True(t, receiver.alreadyVerified(good4.op, layer, v, good4.partial),
		"good tuple must be cache-populated")
	require.Len(t, groups, 1, "good partials on same V → one sigGroup")
	require.Len(t, groups[0].partials, 2, "two good tuples in the group")

	// Bad tuple NOT populated, NOT in any group.
	require.False(t, receiver.alreadyVerified(bad2.op, layer, v, bad2.partial),
		"bad tuple MUST NOT be cache-populated")

	// Bad tuple fires Rule-4 evidence at L_k>0 with FakeEncryptedPresence shape.
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

// TestObft_F4_SequentialVerifyAndAttribute_L0_NoRule4 — at L_0 a failing
// tuple in the fallback does NOT fire Rule-4 evidence (matches the pre-F4
// inline path: Rule 5 is the L_0 attribution rule and fires at observe
// time in ObserveCommit, not in Resolve). At L_0 the fallback is normally
// unreachable (L_0 entries cache-hit and never enter pending) but the
// helper's guard is load-bearing as a defense-in-depth check.
func TestObft_F4_SequentialVerifyAndAttribute_L0_NoRule4(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_0_no_rule4")

	bad := makeStubPendingFor(t, s, receiver, 2, v)
	tampered := append([]byte{}, bad.partial...)
	tampered[0] ^= 0xFF
	bad.partial = tampered

	var groups []*sigGroup
	receiver.sequentialVerifyAndAttribute(0, []pendingVerify{bad}, &groups)

	require.Empty(t, groups, "bad tuple at L_0 must not enter group")
	require.False(t, receiver.alreadyVerified(bad.op, 0, v, bad.partial),
		"bad tuple at L_0 must not be cache-populated")
	require.Empty(t, receiver.Evidence(),
		"L_0 fallback MUST NOT fire Rule-4 evidence (Rule 5 fires at observe time)")
}

// TestObft_F4_SequentialVerifyAndAttribute_DedupPerOpLayer — recordRule4
// dedupes per (op, layer). If the fallback is invoked twice on the same
// (op, layer) with distinct bad partial bytes (mirrors byzantine
// equivocation: two distinct onion entries at the same (op, layer), each
// decrypting to a distinct bad partial), only one Rule-4 evidence fires.
//
// This matches the pre-F4 inline behaviour at base/phase3.go:262.
func TestObft_F4_SequentialVerifyAndAttribute_DedupPerOpLayer(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]
	v := []byte("V_at_layer_1_dedup")
	const layer = 1

	bad1 := makeStubPendingFor(t, s, receiver, 2, v)
	bad2 := makeStubPendingFor(t, s, receiver, 2, v)
	// Tamper differently so the partial bytes differ between the two tuples.
	t1 := append([]byte{}, bad1.partial...)
	t1[0] ^= 0xFF
	bad1.partial = t1
	t2 := append([]byte{}, bad2.partial...)
	t2[1] ^= 0xFF
	bad2.partial = t2

	var groups []*sigGroup
	receiver.sequentialVerifyAndAttribute(layer, []pendingVerify{bad1, bad2}, &groups)
	require.Len(t, receiver.Evidence(), 1,
		"recordRule4 dedupes per (op, layer); only one evidence fires for two distinct bad tuples")
}
