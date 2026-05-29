package twoab

import (
	"testing"

	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Tests for the F4 proposerSigner.VerifyPartialBatch wrapper — twoab mirror
// of protocol/v2/ssv/runner/obft/proposer_signer_test.go. See
// docs/OBFT-F4-IMPLEMENTATION-PLAN.md §proposerSigner wrapper.

// makeTestV builds a minimal [version | SSZ blinded block] candidate that
// the twoab proposerSigner's signingRootFor can decode. Tests in this
// package only need one small valid candidate to exercise the wrapper-
// translate logic; the full attestation-count range is benchmarked in the
// base package (proposer_signer_bench_test.go).
func makeTestV(t *testing.T) []byte {
	return makeTestVForSlot(t, 12345)
}

// makeTestVForSlot is the slot-parameterised twin of makeTestV. Used by F2
// cache tests that need two distinct V's with different sha256 fingerprints.
// Distinct slots produce distinct SSZ byte sequences (the slot field is in
// the marshaled bytes) and therefore distinct cache keys.
func makeTestVForSlot(t *testing.T, slot uint64) []byte {
	t.Helper()
	bb := &apiv1deneb.BlindedBeaconBlock{
		Slot:          phase0.Slot(slot),
		ProposerIndex: 42,
		ParentRoot:    phase0.Root{8},
		StateRoot:     phase0.Root{9},
		Body: &apiv1deneb.BlindedBeaconBlockBody{
			RANDAOReveal: phase0.BLSSignature{},
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{},
				DepositCount: 0,
				BlockHash:    make([]byte, 32),
			},
			Graffiti: [32]byte{7},
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits:      bitfield.Bitvector512(make([]byte, 64)),
				SyncCommitteeSignature: phase0.BLSSignature{},
			},
			ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
				ParentHash:       [32]byte{1},
				FeeRecipient:     [20]byte{2},
				StateRoot:        [32]byte{3},
				ReceiptsRoot:     [32]byte{4},
				PrevRandao:       [32]byte{5},
				BlockNumber:      10,
				GasLimit:         11,
				GasUsed:          12,
				Timestamp:        13,
				ExtraData:        []byte{0xaa, 0xbb},
				BaseFeePerGas:    uint256.NewInt(0),
				BlockHash:        [32]byte{6},
				TransactionsRoot: [32]byte{14},
				WithdrawalsRoot:  [32]byte{15},
			},
		},
	}
	ssz, err := bb.MarshalSSZ()
	require.NoError(t, err)
	return EncodeCandidate(spec.DataVersionDeneb, ssz)
}

// recordingBatchSigner satisfies twoabcore.Signer (= obft.Signer) and
// records its VerifyPartialBatch arguments. Mirror of the base test
// package's mock — see that one's doc-comment for the design rationale.
type recordingBatchSigner struct {
	batchCalls   int
	lastPubs     [][]byte
	lastMsgs     [][]byte
	lastSigs     []twoabcore.Signature
	batchReturns bool
}

func (r *recordingBatchSigner) SignPartial([]byte) (twoabcore.Signature, error) {
	panic("recordingBatchSigner: SignPartial not exercised by VerifyPartialBatch tests")
}

func (r *recordingBatchSigner) AggregatePartials(map[twoabcore.OperatorID]twoabcore.Signature) (twoabcore.Signature, error) {
	panic("recordingBatchSigner: AggregatePartials not exercised by VerifyPartialBatch tests")
}

func (r *recordingBatchSigner) VerifyPartial([]byte, []byte, twoabcore.Signature) bool {
	panic("recordingBatchSigner: VerifyPartial not exercised by VerifyPartialBatch tests")
}

func (r *recordingBatchSigner) VerifyAggregate([]byte, []byte, twoabcore.Signature) bool {
	panic("recordingBatchSigner: VerifyAggregate not exercised by VerifyPartialBatch tests")
}

func (r *recordingBatchSigner) VerifyPartialBatch(pubs [][]byte, msgs [][]byte, sigs []twoabcore.Signature) bool {
	r.batchCalls++
	r.lastPubs = pubs
	r.lastMsgs = msgs
	r.lastSigs = sigs
	return r.batchReturns
}

// newTestProposerSigner builds a twoab proposerSigner wrapping the given
// inner signer. Uses TestNetwork's beacon config (Deneb-aware).
func newTestProposerSigner(t *testing.T, inner twoabcore.Signer) *proposerSigner {
	t.Helper()
	s, err := NewProposerSigner(inner, networkconfig.TestNetwork.Beacon)
	require.NoError(t, err)
	ps, ok := s.(*proposerSigner)
	require.True(t, ok, "expected *proposerSigner concrete type")
	return ps
}

// TestProposerSigner_VerifyPartialBatch_TranslatesEachMsg_DelegatesOnce —
// happy path mirror. Same N tuples sharing one V; assert one delegated
// call, translated 32-byte msgs, identical-V → identical-signing-root
// invariant (the σ-walk shape).
func TestProposerSigner_VerifyPartialBatch_TranslatesEachMsg_DelegatesOnce(t *testing.T) {
	v := makeTestV(t)
	const n = 3
	pubs := make([][]byte, n)
	msgs := make([][]byte, n)
	sigs := make([]twoabcore.Signature, n)
	for i := 0; i < n; i++ {
		pubs[i] = []byte{byte(i + 1)}
		msgs[i] = v
		sigs[i] = twoabcore.Signature{byte(i)}
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
	for i := 1; i < n; i++ {
		require.Equal(t, inner.lastMsgs[0], inner.lastMsgs[i],
			"same V at every tuple → identical translated signing root (F4 σ-walk invariant)")
	}
	// Cache-wiring check: N tuples sharing one V collapse to a single shared
	// proposersig.Cache entry (twoab-side confirmation that signingRootFor
	// delegates to the cache — the mechanism itself is tested in the base
	// adapter's proposer_signer_cache_test.go).
	require.Equal(t, 1, ps.sr.Len(),
		"N tuples sharing one V must produce exactly one cache entry")
}

// TestProposerSigner_VerifyPartialBatch_BadV_FailsWithoutDelegating —
// one bad V in the batch fails the wrapper without invoking inner.
func TestProposerSigner_VerifyPartialBatch_BadV_FailsWithoutDelegating(t *testing.T) {
	goodV := makeTestV(t)
	badV := []byte("not-a-valid-candidate")
	pubs := [][]byte{{1}, {2}, {3}}
	msgs := [][]byte{goodV, badV, goodV}
	sigs := []twoabcore.Signature{{0xa}, {0xb}, {0xc}}

	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.False(t, ps.VerifyPartialBatch(pubs, msgs, sigs))
	require.Equal(t, 0, inner.batchCalls,
		"wrapper MUST NOT invoke inner backend when a msg translation fails")
}

// TestProposerSigner_VerifyPartialBatch_LengthMismatch_FailsWithoutDelegating —
// length disagreement returns false without invoking inner.
func TestProposerSigner_VerifyPartialBatch_LengthMismatch_FailsWithoutDelegating(t *testing.T) {
	v := makeTestV(t)
	pubs := [][]byte{{1}, {2}}
	msgs := [][]byte{v, v, v} // 3 msgs vs 2 pubs
	sigs := []twoabcore.Signature{{0xa}, {0xb}, {0xc}}

	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.False(t, ps.VerifyPartialBatch(pubs, msgs, sigs))
	require.Equal(t, 0, inner.batchCalls)
}

// TestProposerSigner_VerifyPartialBatch_EmptyBatch_FailsWithoutDelegating —
// N=0 returns false; inner not invoked.
func TestProposerSigner_VerifyPartialBatch_EmptyBatch_FailsWithoutDelegating(t *testing.T) {
	inner := &recordingBatchSigner{batchReturns: true}
	ps := newTestProposerSigner(t, inner)

	require.False(t, ps.VerifyPartialBatch(nil, nil, nil))
	require.False(t, ps.VerifyPartialBatch([][]byte{}, [][]byte{}, []twoabcore.Signature{}))
	require.Equal(t, 0, inner.batchCalls)
}
