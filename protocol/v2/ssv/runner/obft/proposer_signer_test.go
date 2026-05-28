package obft

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Tests for the F4 proposerSigner.VerifyPartialBatch wrapper — see
// docs/OBFT-F4-IMPLEMENTATION-PLAN.md §proposerSigner wrapper.
//
// The wrapper takes V bytes per msg and translates each through
// signingRootFor before delegating to the inner signer's batch. The
// translate logic is what these tests target; the inner backend's batch
// behaviour is covered by the blsbackend/multiverify_batch_test.go set.
//
// Tests use a recording mock inner signer (recordingBatchSigner) so the
// wrapper-translate path is exercised without touching bls.MultiVerify —
// no checkptr / -race concerns here.

// recordingBatchSigner satisfies obftcore.Signer and records its
// VerifyPartialBatch arguments so tests can assert what the proposerSigner
// wrapper actually delegated. The other Signer methods panic — they're
// not exercised by VerifyPartialBatch flows.
type recordingBatchSigner struct {
	batchCalls   int
	lastPubs     [][]byte
	lastMsgs     [][]byte
	lastSigs     []obftcore.Signature
	batchReturns bool
}

func (r *recordingBatchSigner) SignPartial([]byte) (obftcore.Signature, error) {
	panic("recordingBatchSigner: SignPartial not exercised by VerifyPartialBatch tests")
}

func (r *recordingBatchSigner) AggregatePartials(map[obftcore.OperatorID]obftcore.Signature) (obftcore.Signature, error) {
	panic("recordingBatchSigner: AggregatePartials not exercised by VerifyPartialBatch tests")
}

func (r *recordingBatchSigner) VerifyPartial(_, _ []byte, _ obftcore.Signature) bool {
	panic("recordingBatchSigner: VerifyPartial not exercised by VerifyPartialBatch tests")
}

func (r *recordingBatchSigner) VerifyAggregate(_, _ []byte, _ obftcore.Signature) bool {
	panic("recordingBatchSigner: VerifyAggregate not exercised by VerifyPartialBatch tests")
}

func (r *recordingBatchSigner) VerifyPartialBatch(pubs [][]byte, msgs [][]byte, sigs []obftcore.Signature) bool {
	r.batchCalls++
	r.lastPubs = pubs
	r.lastMsgs = msgs
	r.lastSigs = sigs
	return r.batchReturns
}

// newTestProposerSigner builds a proposerSigner wrapping the given inner
// signer. Uses TestNetwork's beacon config (same as the bench), which
// understands Deneb candidate decoding.
func newTestProposerSigner(t *testing.T, inner obftcore.Signer) *proposerSigner {
	t.Helper()
	beacon := networkconfig.TestNetwork.Beacon
	s, err := NewProposerSigner(inner, beacon)
	require.NoError(t, err)
	ps, ok := s.(*proposerSigner)
	require.True(t, ok, "expected *proposerSigner concrete type")
	return ps
}

// TestProposerSigner_VerifyPartialBatch_TranslatesEachMsg_DelegatesOnce —
// happy path. The wrapper translates each msg (V bytes) into a 32-byte
// signing root and delegates ONE call to the inner backend's batch with
// the translated msgs. Mirrors the F4 σ-walk shape where N operators sign
// the same V at one layer.
func TestProposerSigner_VerifyPartialBatch_TranslatesEachMsg_DelegatesOnce(t *testing.T) {
	v := makeBenchV(t, 4)
	const n = 3
	pubs := make([][]byte, n)
	msgs := make([][]byte, n)
	sigs := make([]obftcore.Signature, n)
	for i := 0; i < n; i++ {
		pubs[i] = []byte{byte(i + 1)} // opaque to the wrapper; passed through
		msgs[i] = v                   // σ-walk: every tuple shares one V
		sigs[i] = obftcore.Signature{byte(i)}
	}

	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.True(t, ps.VerifyPartialBatch(pubs, msgs, sigs),
		"wrapper must propagate the inner backend's true return")
	require.Equal(t, 1, inner.batchCalls,
		"wrapper must delegate exactly one batch call regardless of N")
	require.Equal(t, pubs, inner.lastPubs, "wrapper must pass pubs through unchanged")
	require.Equal(t, sigs, inner.lastSigs, "wrapper must pass sigs through unchanged")
	require.Len(t, inner.lastMsgs, n, "wrapper must hand the inner backend N msgs")
	for i, m := range inner.lastMsgs {
		require.Len(t, m, 32, "translated msg %d must be exactly 32 bytes (signing root)", i)
	}
	// Every V is the same → every translated signing root must be the same.
	for i := 1; i < n; i++ {
		require.Equal(t, inner.lastMsgs[0], inner.lastMsgs[i],
			"same V at every tuple → identical translated signing root (F4 σ-walk invariant)")
	}
}

// TestProposerSigner_VerifyPartialBatch_BadV_FailsWithoutDelegating —
// when ANY msg's V bytes don't decode, the wrapper returns false WITHOUT
// invoking the inner backend. This matches the per-tuple short-circuit
// semantics of the inner backends' input validation and avoids spending
// a MultiVerify pairing equation on a doomed batch.
func TestProposerSigner_VerifyPartialBatch_BadV_FailsWithoutDelegating(t *testing.T) {
	goodV := makeBenchV(t, 0)
	badV := []byte("not-a-valid-candidate")
	pubs := [][]byte{{1}, {2}, {3}}
	msgs := [][]byte{goodV, badV, goodV}
	sigs := []obftcore.Signature{{0xa}, {0xb}, {0xc}}

	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.False(t, ps.VerifyPartialBatch(pubs, msgs, sigs),
		"bad V in any tuple must fail the batch")
	require.Equal(t, 0, inner.batchCalls,
		"wrapper MUST NOT invoke inner backend when a msg translation fails")
}

// TestProposerSigner_VerifyPartialBatch_LengthMismatch_FailsWithoutDelegating —
// input-validation contract: length disagreement returns false without
// invoking the inner backend.
func TestProposerSigner_VerifyPartialBatch_LengthMismatch_FailsWithoutDelegating(t *testing.T) {
	v := makeBenchV(t, 0)
	pubs := [][]byte{{1}, {2}}
	msgs := [][]byte{v, v, v} // 3 msgs vs 2 pubs
	sigs := []obftcore.Signature{{0xa}, {0xb}, {0xc}}

	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.False(t, ps.VerifyPartialBatch(pubs, msgs, sigs),
		"length mismatch must fail the batch")
	require.Equal(t, 0, inner.batchCalls,
		"wrapper MUST NOT invoke inner on length mismatch")
}

// TestProposerSigner_VerifyPartialBatch_EmptyBatch_FailsWithoutDelegating —
// N=0 returns false (contract: N ≥ 1). The Signer interface contract is
// "ALL N tuples verify"; an empty batch has no positive truth to assert.
func TestProposerSigner_VerifyPartialBatch_EmptyBatch_FailsWithoutDelegating(t *testing.T) {
	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.False(t, ps.VerifyPartialBatch(nil, nil, nil))
	require.False(t, ps.VerifyPartialBatch([][]byte{}, [][]byte{}, []obftcore.Signature{}))
	require.Equal(t, 0, inner.batchCalls,
		"wrapper MUST NOT invoke inner on empty batch")
}
