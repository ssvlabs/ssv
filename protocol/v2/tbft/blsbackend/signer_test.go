package blsbackend

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// Tests for the herumi/bls-backed Signer. Critical properties:
//
//   1. Round-trip: partial sigs from `q` distinct operators aggregate to a
//      signature that verifies against the master pubkey for the same
//      message — this is the standard BLS threshold property.
//
//   2. **Any 2f+1 subset yields the SAME aggregate** — the cluster
//      consistency property. Two operators with different (but each
//      quorum-sized) subsets of partials must derive identical decision
//      signatures, otherwise consensus breaks.
//
//   3. Verification rejects forged or wrong-key partials.

// keyset is a generated threshold-BLS keypair plus all operator shares.
type keyset struct {
	master    *bls.SecretKey // never used by operators directly
	masterPub *bls.PublicKey // cluster pubkey (validator pubkey under Option A)
	shares    map[uint64]*bls.SecretKey
	pubShares map[uint64]*bls.PublicKey
	q         int // 2f+1 quorum threshold
	n         int // cluster size
}

func newKeyset(t *testing.T, n int) *keyset {
	t.Helper()
	threshold.Init() // ensure herumi is initialized
	f := (n - 1) / 3
	q := 2*f + 1
	require.Equal(t, n, 3*f+1, "test sizes assume n = 3f+1")

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	pubShares := make(map[uint64]*bls.PublicKey, n)
	for id, sk := range shares {
		pubShares[id] = sk.GetPublicKey()
	}

	return &keyset{
		master:    master,
		masterPub: masterPub,
		shares:    shares,
		pubShares: pubShares,
		q:         q,
		n:         n,
	}
}

// signWith produces a partial sig from operator `id` over `msg`.
func (k *keyset) signWith(t *testing.T, signer *BLSSigner, id uint64, msg []byte) tbft.Signature {
	t.Helper()
	share := k.shares[id]
	require.NotNil(t, share, "no share for id %d", id)
	p, err := signer.SignPartial(share.Serialize(), msg)
	require.NoError(t, err)
	return p
}

func TestSigner_RoundTrip_n7(t *testing.T) {
	ks := newKeyset(t, 7)
	signer := New()

	msg := []byte("hello, threshold world")

	// Operators 1..q sign.
	partials := make(map[tbft.OperatorID]tbft.Signature, ks.q)
	for id := uint64(1); id <= uint64(ks.q); id++ {
		partials[tbft.OperatorID(id)] = ks.signWith(t, signer, id, msg)
	}

	full, err := signer.AggregatePartials(partials)
	require.NoError(t, err)

	// Reconstructed signature verifies against the master pubkey.
	require.True(t, signer.VerifyAggregate(ks.masterPub.Serialize(), msg, full),
		"aggregated signature should verify under the master/cluster pubkey")

	// Sanity: it should EQUAL what the master key would have signed itself.
	masterSig := ks.master.SignByte(msg)
	require.True(t, bytes.Equal(masterSig.Serialize(), full),
		"reconstructed signature should equal the master's direct signature")
}

func TestSigner_AnyQuorumSubsetYieldsSameAggregate(t *testing.T) {
	// THE critical TBFT-cluster property under Option A.
	ks := newKeyset(t, 7) // q=5, n=7
	signer := New()

	msg := []byte("the cluster's decision")

	// Generate all 7 partials; we'll pick different 5-element subsets.
	allPartials := make(map[tbft.OperatorID]tbft.Signature, ks.n)
	for id := uint64(1); id <= uint64(ks.n); id++ {
		allPartials[tbft.OperatorID(id)] = ks.signWith(t, signer, id, msg)
	}

	subset1 := mapSubset(allPartials, 1, 2, 3, 4, 5) // first five
	subset2 := mapSubset(allPartials, 3, 4, 5, 6, 7) // last five
	subset3 := mapSubset(allPartials, 1, 3, 5, 6, 7) // mixed

	agg1, err := signer.AggregatePartials(subset1)
	require.NoError(t, err)
	agg2, err := signer.AggregatePartials(subset2)
	require.NoError(t, err)
	agg3, err := signer.AggregatePartials(subset3)
	require.NoError(t, err)

	require.True(t, bytes.Equal(agg1, agg2),
		"distinct quorum subsets must aggregate to the SAME signature (subset 1 vs 2)")
	require.True(t, bytes.Equal(agg2, agg3),
		"distinct quorum subsets must aggregate to the SAME signature (subset 2 vs 3)")

	// And all aggregations verify against the same master pubkey.
	require.True(t, signer.VerifyAggregate(ks.masterPub.Serialize(), msg, agg1))
}

func TestSigner_VerifyPartial(t *testing.T) {
	ks := newKeyset(t, 7)
	signer := New()
	msg := []byte("verify me")

	p := ks.signWith(t, signer, 3, msg)

	// Correct pubkey share verifies.
	require.True(t, signer.VerifyPartial(ks.pubShares[3].Serialize(), msg, p))
	// Wrong pubkey share does NOT verify.
	require.False(t, signer.VerifyPartial(ks.pubShares[4].Serialize(), msg, p))
	// Wrong message does NOT verify.
	require.False(t, signer.VerifyPartial(ks.pubShares[3].Serialize(), []byte("other"), p))
}

func TestSigner_AggregateRejectsBelowQuorum(t *testing.T) {
	// herumi's Recover will produce *some* output for any number of partials
	// — including one that won't verify against the master pubkey. We don't
	// expect an error from AggregatePartials; the caller is responsible for
	// counting the partials. Verify the resulting signature does NOT match
	// the master signature.
	ks := newKeyset(t, 7) // q=5
	signer := New()
	msg := []byte("not enough partials")

	// Only 4 partials (one short).
	partials := make(map[tbft.OperatorID]tbft.Signature, 4)
	for id := uint64(1); id <= 4; id++ {
		partials[tbft.OperatorID(id)] = ks.signWith(t, signer, id, msg)
	}

	bogus, err := signer.AggregatePartials(partials)
	require.NoError(t, err, "below-quorum aggregation produces output but it shouldn't verify")
	require.False(t, signer.VerifyAggregate(ks.masterPub.Serialize(), msg, bogus),
		"below-quorum aggregate must not verify against the master pubkey")
}

func TestSigner_AggregateRejectsCrossMessagePartials(t *testing.T) {
	// Partials on different messages don't aggregate to a meaningful signature.
	// herumi may produce SOMETHING, but it won't verify.
	ks := newKeyset(t, 4) // q=3
	signer := New()
	msgA := []byte("message a")
	msgB := []byte("message b")

	partials := map[tbft.OperatorID]tbft.Signature{
		tbft.OperatorID(1): ks.signWith(t, signer, 1, msgA),
		tbft.OperatorID(2): ks.signWith(t, signer, 2, msgA),
		tbft.OperatorID(3): ks.signWith(t, signer, 3, msgB), // wrong message
	}

	bogus, err := signer.AggregatePartials(partials)
	require.NoError(t, err)
	require.False(t, signer.VerifyAggregate(ks.masterPub.Serialize(), msgA, bogus))
	require.False(t, signer.VerifyAggregate(ks.masterPub.Serialize(), msgB, bogus))
}

func TestSigner_EmptyInputs(t *testing.T) {
	signer := New()

	_, err := signer.SignPartial(nil, []byte("msg"))
	require.Error(t, err)

	_, err = signer.SignPartial([]byte("share"), nil)
	require.Error(t, err)

	_, err = signer.AggregatePartials(map[tbft.OperatorID]tbft.Signature{})
	require.Error(t, err)
}

// ---- helpers -------------------------------------------------------------

func mapSubset(m map[tbft.OperatorID]tbft.Signature, ids ...uint64) map[tbft.OperatorID]tbft.Signature {
	out := make(map[tbft.OperatorID]tbft.Signature, len(ids))
	for _, id := range ids {
		opID := tbft.OperatorID(id)
		v, ok := m[opID]
		if !ok {
			panic(fmt.Sprintf("mapSubset: missing key %d", id))
		}
		out[opID] = v
	}
	return out
}
