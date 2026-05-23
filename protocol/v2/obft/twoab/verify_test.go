package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 2abOBFT Verifier unit tests. Messages are hand-rolled with the stub signer
// (whose VerifyPartial is symmetric: a partial signed with share {op} verifies
// against pub-share {op}), so each test pins exactly one crypto check without
// dragging in the protocol sim. Mirrors base/verify_test.go in spirit, adapted
// to the 2ab message shapes (σ on ValueMsg.L0Partial, NR on Commit.L0Partial +
// NRPlaintext LayerEntries).

const verifyQ = 3 // 4-op cluster: q = 2f+1

var (
	verifyClusterID = [32]byte{0xAB, 0xCD}
	verifyHeight    = Height(7)
)

func verifierShares(n int) map[OperatorID][]byte {
	m := make(map[OperatorID][]byte, n)
	for op := 1; op <= n; op++ {
		m[OperatorID(op)] = []byte{byte(op)}
	}
	return m
}

func newTestVerifier(pub, nr map[OperatorID][]byte) *Verifier {
	return &Verifier{
		Signer:         NewStubSigner(verifyQ, nil), // verify-only
		TagSigner:      NewStubSigner(verifyQ, nil),
		PubKeyShares:   pub,
		NRPubKeyShares: nr,
		ClusterPubKey:  nil, // stub VerifyAggregate ignores it
	}
}

// validSigma is op's valid σ partial on v (stub share = {byte(op)}).
func validSigma(t *testing.T, op OperatorID, v Value) Signature {
	t.Helper()
	sig, err := NewStubSigner(verifyQ, []byte{byte(op)}).SignPartial(v)
	require.NoError(t, err)
	return sig
}

// validNR is op's valid nr_tag_layer IBE partial (stub share = {byte(op)}).
func validNR(t *testing.T, op OperatorID, layer int) Signature {
	t.Helper()
	tag := NoQuorumTag(verifyClusterID, verifyHeight, layer)
	sig, err := NewStubSigner(verifyQ, []byte{byte(op)}).SignPartial(tag)
	require.NoError(t, err)
	return sig
}

// validAggregate is a valid full signature on v (q distinct partials).
func validAggregate(t *testing.T, v Value) Signature {
	t.Helper()
	parts := make(map[OperatorID]Signature, verifyQ)
	for op := 1; op <= verifyQ; op++ {
		parts[OperatorID(op)] = validSigma(t, OperatorID(op), v)
	}
	agg, err := NewStubSigner(verifyQ, nil).AggregatePartials(parts)
	require.NoError(t, err)
	return agg
}

// --- Phase1Bundle (shared obft type; identical surface to base) ---

func TestVerifier_Phase1Bundle_AcceptsValid(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	b := &Phase1Bundle{
		ClusterID: verifyClusterID, OperatorID: 1, Height: verifyHeight, Layer: 0,
		Value: Value("V"), LeaderSigma: validSigma(t, 1, Value("V")),
	}
	require.NoError(t, v.VerifyPhase1Bundle(b))
}

func TestVerifier_Phase1Bundle_RejectsCorrupt(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	b := &Phase1Bundle{
		OperatorID: 1, Value: Value("V"), LeaderSigma: Signature("garbage-not-a-partial"),
	}
	require.ErrorContains(t, v.VerifyPhase1Bundle(b), "does not verify")
}

func TestVerifier_Phase1Bundle_RejectsUnknownOperator(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	b := &Phase1Bundle{OperatorID: 99, Value: Value("V"), LeaderSigma: validSigma(t, 99, Value("V"))}
	require.ErrorContains(t, v.VerifyPhase1Bundle(b), "no pub-key share")
}

// --- ValueMsg (σ on L0Partial + optional NRPlaintext entries) ---

func TestVerifier_ValueMsg_AcceptsValid(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	m := &ValueMsg{
		ClusterID: verifyClusterID, OperatorID: 2, Height: verifyHeight,
		V: Value("V"), ValueRoot: ValueRoot(Value("V")), L0Partial: validSigma(t, 2, Value("V")),
	}
	require.NoError(t, v.VerifyValueMsg(m))
}

func TestVerifier_ValueMsg_AcceptsValidWithNRLayerEntries(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	m := &ValueMsg{
		ClusterID: verifyClusterID, OperatorID: 2, Height: verifyHeight,
		V: Value("V"), ValueRoot: ValueRoot(Value("V")), L0Partial: validSigma(t, 2, Value("V")),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryNRPlaintext, Payload: validNR(t, 2, 1)},
		},
	}
	require.NoError(t, v.VerifyValueMsg(m))
}

func TestVerifier_ValueMsg_RejectsCorruptL0Partial(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	m := &ValueMsg{OperatorID: 2, V: Value("V"), L0Partial: Signature("garbage")}
	require.ErrorContains(t, v.VerifyValueMsg(m), "σ partial from op 2 does not verify")
}

func TestVerifier_ValueMsg_RejectsUnknownOperator(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	m := &ValueMsg{OperatorID: 99, V: Value("V"), L0Partial: validSigma(t, 99, Value("V"))}
	require.ErrorContains(t, v.VerifyValueMsg(m), "no pub-key share")
}

func TestVerifier_ValueMsg_RejectsCorruptNRLayerEntry(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	m := &ValueMsg{
		ClusterID: verifyClusterID, OperatorID: 2, Height: verifyHeight,
		V: Value("V"), L0Partial: validSigma(t, 2, Value("V")),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryNRPlaintext, Payload: Signature("garbage-nr")},
		},
	}
	require.ErrorContains(t, v.VerifyValueMsg(m), "NR partial (entry 0, layer 1) from op 2 does not verify")
}

// Anti-framing contract: forwarded Witnesses are NOT verified at the boundary
// (a bad forwarded witness is silently dropped at the protocol layer, never
// attributed to anyone), so a ValueMsg with garbage Witnesses but a valid
// L0Partial must still pass. Mirrors base's CommitWitnesses_StructuralOnly.
func TestVerifier_ValueMsg_DoesNotVerifyWitnesses(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	m := &ValueMsg{
		ClusterID: verifyClusterID, OperatorID: 2, Height: verifyHeight,
		V: Value("V"), L0Partial: validSigma(t, 2, Value("V")),
		Witnesses: []LayerWitness{
			{Layer: 0, ValueRoot: ValueRoot(Value("V")), Witness: Signature("garbage-witness")},
		},
	}
	require.NoError(t, v.VerifyValueMsg(m),
		"forwarded witnesses are intentionally not verified at the validation boundary")
}

// --- NoValueMsg (NRPlaintext entries only; no L_0 payload) ---

func TestVerifier_NoValueMsg_AcceptsValidNRLayerEntries(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	m := &NoValueMsg{
		ClusterID: verifyClusterID, OperatorID: 3, Height: verifyHeight,
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryNRPlaintext, Payload: validNR(t, 3, 1)},
			{Layer: 2, Kind: LayerEntryEmpty}, // skipped — nothing to verify
		},
	}
	require.NoError(t, v.VerifyNoValueMsg(m))
}

func TestVerifier_NoValueMsg_AcceptsNoNREntries(t *testing.T) {
	// All entries Empty / SigmaChained → no NR share required, no verify.
	v := newTestVerifier(verifierShares(4), nil)
	m := &NoValueMsg{
		ClusterID: verifyClusterID, OperatorID: 3, Height: verifyHeight,
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryEmpty},
			{Layer: 2, Kind: LayerEntrySigmaChained, V: Value("V"), Payload: Signature("chained-opaque")},
		},
	}
	require.NoError(t, v.VerifyNoValueMsg(m))
}

func TestVerifier_NoValueMsg_RejectsCorruptNRLayerEntry(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	m := &NoValueMsg{
		ClusterID: verifyClusterID, OperatorID: 3, Height: verifyHeight,
		LayerEntries: []LayerEntry{
			{Layer: 2, Kind: LayerEntryNRPlaintext, Payload: Signature("garbage")},
		},
	}
	require.ErrorContains(t, v.VerifyNoValueMsg(m), "NR partial (entry 0, layer 2) from op 3 does not verify")
}

// --- Commit (nr_tag_0 on L0Partial + optional NRPlaintext for NRDirect) ---

func TestVerifier_Commit_AcceptsValid(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	c := &Commit{
		ClusterID: verifyClusterID, OperatorID: 2, Height: verifyHeight,
		Side: CommitSideNR, L0Partial: validNR(t, 2, 0),
	}
	require.NoError(t, v.VerifyCommit(c))
}

func TestVerifier_Commit_AcceptsValidNRDirect(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	c := &Commit{
		ClusterID: verifyClusterID, OperatorID: 2, Height: verifyHeight,
		Side: CommitSideNRDirect, L0Partial: validNR(t, 2, 0),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryNRPlaintext, Payload: validNR(t, 2, 1)},
		},
	}
	require.NoError(t, v.VerifyCommit(c))
}

func TestVerifier_Commit_RejectsCorruptL0Partial(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	c := &Commit{ClusterID: verifyClusterID, OperatorID: 2, Height: verifyHeight, Side: CommitSideNR, L0Partial: Signature("garbage")}
	require.ErrorContains(t, v.VerifyCommit(c), "commit L_0 nr_tag partial from op 2 does not verify")
}

func TestVerifier_Commit_RejectsUnknownOperator(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	c := &Commit{ClusterID: verifyClusterID, OperatorID: 99, Height: verifyHeight, Side: CommitSideNR, L0Partial: validNR(t, 99, 0)}
	require.ErrorContains(t, v.VerifyCommit(c), "no NR pub-key share")
}

// Option B: a separate IBE keypair (NRPubKeyShares populated). The NR partial
// must verify against the NR shares, not the V shares.
func TestVerifier_Commit_OptionB_NRShares(t *testing.T) {
	// V shares for ops 1..4, but NR shares only for op 2 with a DISTINCT share
	// byte so we prove the NR path consults NRPubKeyShares.
	nr := map[OperatorID][]byte{2: {0x42}}
	v := newTestVerifier(verifierShares(4), nr)
	tag := NoQuorumTag(verifyClusterID, verifyHeight, 0)
	nrSig, err := NewStubSigner(verifyQ, []byte{0x42}).SignPartial(tag)
	require.NoError(t, err)
	c := &Commit{ClusterID: verifyClusterID, OperatorID: 2, Height: verifyHeight, Side: CommitSideNR, L0Partial: nrSig}
	require.NoError(t, v.VerifyCommit(c))

	// The V-share partial (share {2}) must NOT verify under Option B NR shares.
	c.L0Partial = validNR(t, 2, 0) // signed with share {2}, but NR share is {0x42}
	require.ErrorContains(t, v.VerifyCommit(c), "does not verify")
}

// --- Certificate (shared obft type; identical surface to base) ---

func TestVerifier_Certificate_AcceptsValid(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	c := &Certificate{Value: Value("V"), Signature: validAggregate(t, Value("V"))}
	require.NoError(t, v.VerifyCertificate(c))
}

func TestVerifier_Certificate_RejectsCorrupt(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	c := &Certificate{Value: Value("V"), Signature: Signature("garbage-aggregate")}
	require.ErrorContains(t, v.VerifyCertificate(c), "does not verify")
}

// --- misconfiguration guards ---

func TestVerifier_RejectsMisconfigured(t *testing.T) {
	require.ErrorContains(t, (*Verifier)(nil).VerifyPhase1Bundle(&Phase1Bundle{}), "nil verifier")

	v := &Verifier{Signer: nil, TagSigner: NewStubSigner(verifyQ, nil)}
	require.ErrorContains(t, v.VerifyPhase1Bundle(&Phase1Bundle{}), "nil Signer")

	v = &Verifier{Signer: NewStubSigner(verifyQ, nil), TagSigner: nil}
	require.ErrorContains(t, v.VerifyCommit(&Commit{}), "nil TagSigner")
}

func TestVerifier_NilMessages(t *testing.T) {
	v := newTestVerifier(verifierShares(4), nil)
	require.ErrorContains(t, v.VerifyPhase1Bundle(nil), "nil phase-1 bundle")
	require.ErrorContains(t, v.VerifyValueMsg(nil), "nil value msg")
	require.ErrorContains(t, v.VerifyNoValueMsg(nil), "nil no-value msg")
	require.ErrorContains(t, v.VerifyCommit(nil), "nil commit")
	require.ErrorContains(t, v.VerifyCertificate(nil), "nil certificate")
}
