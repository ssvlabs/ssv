package blsbackend

import (
	"bytes"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// KyberSigner tests parallel BLSSigner's tests, exercising the same
// contract: the SAME 2f+1 subset of partials must aggregate to the SAME
// signature, verifiable under the master pubkey.

// kyberKeyset is the kyber-side analog of `keyset` (in signer_test.go).
// It generates herumi-style threshold shares and exposes them as bytes —
// the KyberSigner consumes herumi-formatted bytes directly via the
// conversion functions.
type kyberKeyset struct {
	masterPub []byte            // herumi-format G1 pubkey bytes (48)
	shares    map[uint64][]byte // operator-share-bytes (32 each)
	pubShares map[uint64][]byte // operator-pubkey-bytes (48 each)
	q, n      int
	masterSK  *bls.SecretKey // kept for direct verification only
}

func newKyberKeyset(t *testing.T, n int) *kyberKeyset {
	t.Helper()
	threshold.Init()
	f := (n - 1) / 3
	q := 2*f + 1
	require.Equal(t, n, 3*f+1)

	master := &bls.SecretKey{}
	master.SetByCSPRNG()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	shareBytes := make(map[uint64][]byte, n)
	pubBytes := make(map[uint64][]byte, n)
	for id, sk := range shares {
		shareBytes[id] = sk.Serialize()
		pubBytes[id] = sk.GetPublicKey().Serialize()
	}

	return &kyberKeyset{
		masterPub: master.GetPublicKey().Serialize(),
		shares:    shareBytes,
		pubShares: pubBytes,
		q:         q,
		n:         n,
		masterSK:  master,
	}
}

func TestKyberSigner_RoundTrip_n7(t *testing.T) {
	ks := newKyberKeyset(t, 7)
	signer := NewKyberSigner()

	msg := []byte("a-no-quorum-tag")

	partials := make(map[tbft.OperatorID]tbft.Signature, ks.q)
	for id := uint64(1); id <= uint64(ks.q); id++ {
		p, err := signer.SignPartial(ks.shares[id], msg)
		require.NoError(t, err)
		partials[tbft.OperatorID(id)] = p
	}

	full, err := signer.AggregatePartials(partials)
	require.NoError(t, err)

	// Verify under the master pubkey.
	require.True(t, signer.VerifyAggregate(ks.masterPub, msg, full),
		"kyber-aggregated signature must verify under herumi-derived master pubkey")
}

func TestKyberSigner_AnyQuorumSubsetYieldsSameAggregate(t *testing.T) {
	// THE critical TBFT-cluster property, mirrored to kyber.
	ks := newKyberKeyset(t, 7) // q=5, n=7
	signer := NewKyberSigner()
	msg := []byte("the-cluster's-decision-tag")

	all := make(map[tbft.OperatorID]tbft.Signature, ks.n)
	for id := uint64(1); id <= uint64(ks.n); id++ {
		p, err := signer.SignPartial(ks.shares[id], msg)
		require.NoError(t, err)
		all[tbft.OperatorID(id)] = p
	}

	subset1 := mapSubset(all, 1, 2, 3, 4, 5)
	subset2 := mapSubset(all, 3, 4, 5, 6, 7)
	subset3 := mapSubset(all, 1, 3, 5, 6, 7)

	a1, err := signer.AggregatePartials(subset1)
	require.NoError(t, err)
	a2, err := signer.AggregatePartials(subset2)
	require.NoError(t, err)
	a3, err := signer.AggregatePartials(subset3)
	require.NoError(t, err)

	require.True(t, bytes.Equal(a1, a2),
		"distinct quorum subsets must produce identical kyber aggregates")
	require.True(t, bytes.Equal(a2, a3),
		"distinct quorum subsets must produce identical kyber aggregates")

	require.True(t, signer.VerifyAggregate(ks.masterPub, msg, a1))
}

func TestKyberSigner_VerifyPartial(t *testing.T) {
	ks := newKyberKeyset(t, 7)
	signer := NewKyberSigner()
	msg := []byte("verify-me")

	p, err := signer.SignPartial(ks.shares[3], msg)
	require.NoError(t, err)

	require.True(t, signer.VerifyPartial(ks.pubShares[3], msg, p))
	require.False(t, signer.VerifyPartial(ks.pubShares[4], msg, p),
		"wrong pubkey share must not verify")
	require.False(t, signer.VerifyPartial(ks.pubShares[3], []byte("other"), p),
		"wrong message must not verify")
}

func TestKyberSigner_AggregateRejectsBelowQuorum(t *testing.T) {
	ks := newKyberKeyset(t, 7)
	signer := NewKyberSigner()
	msg := []byte("not-enough-partials")

	partials := make(map[tbft.OperatorID]tbft.Signature, 4)
	for id := uint64(1); id <= 4; id++ {
		p, err := signer.SignPartial(ks.shares[id], msg)
		require.NoError(t, err)
		partials[tbft.OperatorID(id)] = p
	}
	bogus, err := signer.AggregatePartials(partials)
	require.NoError(t, err, "below-quorum aggregation produces output but it shouldn't verify")
	require.False(t, signer.VerifyAggregate(ks.masterPub, msg, bogus),
		"below-quorum aggregate must not verify under the master pubkey")
}

func TestKyberSigner_EmptyInputs(t *testing.T) {
	signer := NewKyberSigner()
	_, err := signer.SignPartial(nil, []byte("m"))
	require.Error(t, err)
	_, err = signer.SignPartial(make([]byte, HerumiSecretShareSize), nil)
	require.Error(t, err)
	_, err = signer.AggregatePartials(map[tbft.OperatorID]tbft.Signature{})
	require.Error(t, err)
}
