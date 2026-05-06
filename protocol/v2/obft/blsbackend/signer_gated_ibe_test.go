package blsbackend

import (
	"bytes"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// SignerGatedIBE tests focus on the access-gate behavior. Confidentiality
// is intentionally not tested (it's not a property the implementation
// claims).

// signTag returns operator `id`'s partial sig on `tag` using a freshly-
// constructed share-bound BLSSigner.
func signTag(t *testing.T, shares map[uint64]*bls.SecretKey, id uint64, tag []byte) obft.Signature {
	t.Helper()
	op := New(shares[id].Serialize())
	p, err := op.SignPartial(tag)
	require.NoError(t, err)
	return p
}

func TestSignerGatedIBE_RoundTripWithBLS(t *testing.T) {
	threshold.Init()
	const n, q = 7, 5

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()

	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	verifier := New(nil)
	ibe := NewSignerGatedIBE(verifier, masterPub)

	tag := []byte("test-tag")
	plaintext := []byte("the cluster's secret payload")

	// Encrypt — pubkey arg is unused in this stub (Verifier holds the cluster pubkey).
	ct, err := ibe.Encrypt(nil, tag, plaintext)
	require.NoError(t, err)

	// Build a valid decryption key: q operators sign the tag and aggregate.
	partials := make(map[obft.OperatorID]obft.Signature, q)
	for id := uint64(1); id <= q; id++ {
		partials[obft.OperatorID(id)] = signTag(t, shares, id, tag)
	}
	key, err := verifier.AggregatePartials(partials)
	require.NoError(t, err)

	got, err := ibe.Decrypt(ct, key)
	require.NoError(t, err)
	require.True(t, bytes.Equal(got, plaintext))
}

func TestSignerGatedIBE_RejectsKeyForDifferentTag(t *testing.T) {
	threshold.Init()
	const n, q = 7, 5

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	verifier := New(nil)
	ibe := NewSignerGatedIBE(verifier, masterPub)

	ct, err := ibe.Encrypt(nil, []byte("real-tag"), []byte("plaintext"))
	require.NoError(t, err)

	// Build a key signed over a DIFFERENT tag.
	partials := make(map[obft.OperatorID]obft.Signature, q)
	for id := uint64(1); id <= q; id++ {
		partials[obft.OperatorID(id)] = signTag(t, shares, id, []byte("wrong-tag"))
	}
	wrongKey, _ := verifier.AggregatePartials(partials)

	_, err = ibe.Decrypt(ct, wrongKey)
	require.ErrorContains(t, err, "not a valid signature on the ciphertext's tag")
}

func TestSignerGatedIBE_RejectsBelowQuorumKey(t *testing.T) {
	threshold.Init()
	const n, q = 7, 5

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	verifier := New(nil)
	ibe := NewSignerGatedIBE(verifier, masterPub)

	tag := []byte("test-tag")
	ct, err := ibe.Encrypt(nil, tag, []byte("plaintext"))
	require.NoError(t, err)

	// Aggregate only 4 partials (one short of quorum). herumi's Recover
	// produces *something* but it won't verify under the master pubkey.
	partials := make(map[obft.OperatorID]obft.Signature, 4)
	for id := uint64(1); id <= 4; id++ {
		partials[obft.OperatorID(id)] = signTag(t, shares, id, tag)
	}
	bogusKey, _ := verifier.AggregatePartials(partials)

	_, err = ibe.Decrypt(ct, bogusKey)
	require.Error(t, err, "below-quorum aggregate must not unlock decryption")
}

func TestSignerGatedIBE_MalformedCiphertext(t *testing.T) {
	threshold.Init()
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	verifier := New(nil)
	ibe := NewSignerGatedIBE(verifier, master.GetPublicKey().Serialize())

	tests := []struct {
		name string
		ct   []byte
	}{
		{"empty", nil},
		{"too short", []byte{0x04, 0x00}},
		{"bad version", []byte{0x99, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{"truncated tag", []byte{0x04, 0x00, 0x10, 0x01, 0x02}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ibe.Decrypt(tc.ct, []byte("any-key"))
			require.Error(t, err)
		})
	}
}

func TestSignerGatedIBE_NoVerifier(t *testing.T) {
	ibe := &SignerGatedIBE{Verifier: nil, ClusterPubKey: []byte("pk")}
	ct, _ := (&SignerGatedIBE{}).Encrypt(nil, []byte("tag"), []byte("pt"))
	_, err := ibe.Decrypt(ct, []byte("key"))
	require.ErrorContains(t, err, "no Verifier")
}
