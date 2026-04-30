package blsbackend

import (
	"bytes"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// TLockIBE tests verify the end-to-end "DST trick" composition:
//
//   1. Operators each have a herumi-format BLS share (existing validator share).
//   2. They sign the IBE tag using KyberSigner (which interprets their
//      herumi share bytes as a kyber scalar and signs with kyber's DST).
//   3. 2f+1 such partials aggregate to a kyber-format BLS sig on the tag.
//   4. That aggregate decrypts a TLockIBE.Encrypt'd ciphertext bound to
//      the validator's herumi-format pubkey + same tag.
//
// If this round-trip works, no separate IBE DKG is needed: existing SSV
// operator shares can serve both validator-output signing AND IBE-tag
// signing. See docs/IBE-INTEGRATION.md.

// kyberSignTag returns a kyber partial sig from operator `id` over `tag`,
// using a freshly-constructed share-bound KyberSigner.
func kyberSignTag(t *testing.T, shares map[uint64]*bls.SecretKey, id uint64, tag []byte) tbft.Signature {
	t.Helper()
	op := NewKyberSigner(shares[id].Serialize())
	p, err := op.SignPartial(tag)
	require.NoError(t, err)
	return p
}

func TestTLockIBE_RoundTripWithHerumiShares(t *testing.T) {
	threshold.Init()
	const n, q = 7, 5

	// Existing herumi setup (the validator's threshold key).
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	verifier := NewKyberSigner(nil)
	ibeImpl := NewTLockIBE()

	tag := []byte("tbft-no-quorum-tag-for-slot-100-layer-0")
	plaintext := []byte("this is a partial signature blob (96 bytes in real use; smaller for the test)")

	// Encrypt under the validator's master pubkey.
	ciphertext, err := ibeImpl.Encrypt(masterPub, tag, plaintext)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)
	require.NotEqual(t, ciphertext, plaintext, "ciphertext must differ from plaintext")

	// q operators sign the tag using their herumi shares (via kyber).
	partials := make(map[tbft.OperatorID]tbft.Signature, q)
	for id := uint64(1); id <= q; id++ {
		partials[tbft.OperatorID(id)] = kyberSignTag(t, shares, id, tag)
	}

	// Aggregate to derive the kyber-format decryption key.
	key, err := verifier.AggregatePartials(partials)
	require.NoError(t, err)

	// Decrypt using the aggregated kyber sig.
	got, err := ibeImpl.Decrypt(ciphertext, key)
	require.NoError(t, err)
	require.True(t, bytes.Equal(got, plaintext),
		"TLockIBE decrypt should recover the original plaintext")
}

func TestTLockIBE_DifferentSubsetsDecryptIdentically(t *testing.T) {
	// Critical: different 2f+1 subsets must yield the SAME decryption key,
	// hence the SAME decrypted plaintext, for cluster-wide consistency.
	threshold.Init()
	const n, q = 7, 5
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	verifier := NewKyberSigner(nil)
	ibeImpl := NewTLockIBE()

	tag := []byte("a-tag")
	plaintext := []byte("payload")

	ct, err := ibeImpl.Encrypt(masterPub, tag, plaintext)
	require.NoError(t, err)

	partials := make(map[tbft.OperatorID]tbft.Signature, n)
	for id := uint64(1); id <= n; id++ {
		partials[tbft.OperatorID(id)] = kyberSignTag(t, shares, id, tag)
	}

	subset1 := mapSubset(partials, 1, 2, 3, 4, 5)
	subset2 := mapSubset(partials, 3, 4, 5, 6, 7)

	key1, err := verifier.AggregatePartials(subset1)
	require.NoError(t, err)
	key2, err := verifier.AggregatePartials(subset2)
	require.NoError(t, err)

	pt1, err := ibeImpl.Decrypt(ct, key1)
	require.NoError(t, err)
	pt2, err := ibeImpl.Decrypt(ct, key2)
	require.NoError(t, err)

	require.True(t, bytes.Equal(pt1, pt2), "different quorum subsets must decrypt to the same plaintext")
	require.True(t, bytes.Equal(pt1, plaintext))
}

func TestTLockIBE_WrongTagKeyFails(t *testing.T) {
	// Decryption with a sig over the wrong tag must fail. (Not "produce
	// garbage", but cleanly fail at the AES-GCM auth step.)
	threshold.Init()
	const n, q = 7, 5
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	verifier := NewKyberSigner(nil)
	ibeImpl := NewTLockIBE()

	ct, err := ibeImpl.Encrypt(masterPub, []byte("real-tag"), []byte("plaintext"))
	require.NoError(t, err)

	// Build a key for a DIFFERENT tag.
	partials := make(map[tbft.OperatorID]tbft.Signature, q)
	for id := uint64(1); id <= q; id++ {
		partials[tbft.OperatorID(id)] = kyberSignTag(t, shares, id, []byte("wrong-tag"))
	}
	wrongKey, err := verifier.AggregatePartials(partials)
	require.NoError(t, err)

	_, err = ibeImpl.Decrypt(ct, wrongKey)
	require.Error(t, err, "decryption with sig over wrong tag must fail")
}

func TestTLockIBE_BelowQuorumKeyFails(t *testing.T) {
	threshold.Init()
	const n, q = 7, 5
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()
	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	verifier := NewKyberSigner(nil)
	ibeImpl := NewTLockIBE()

	tag := []byte("tag")
	ct, err := ibeImpl.Encrypt(masterPub, tag, []byte("plaintext"))
	require.NoError(t, err)

	// Aggregate only 4 partials (below quorum).
	partials := make(map[tbft.OperatorID]tbft.Signature, 4)
	for id := uint64(1); id <= 4; id++ {
		partials[tbft.OperatorID(id)] = kyberSignTag(t, shares, id, tag)
	}
	bogusKey, err := verifier.AggregatePartials(partials)
	require.NoError(t, err)

	_, err = ibeImpl.Decrypt(ct, bogusKey)
	require.Error(t, err, "below-quorum aggregate must not decrypt")
}

func TestTLockIBE_MalformedCiphertext(t *testing.T) {
	ibeImpl := NewTLockIBE()
	tests := []struct {
		name string
		ct   []byte
	}{
		{"empty", nil},
		{"bad version", []byte{0xFF, 0x00}},
		{"truncated", []byte{tlockIBEVersionV1, 0x00, 0x10}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ibeImpl.Decrypt(tc.ct, []byte("any-key"))
			require.Error(t, err)
		})
	}
}

func TestTLockIBE_EmptyTag(t *testing.T) {
	ibeImpl := NewTLockIBE()
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	_, err := ibeImpl.Encrypt(master.GetPublicKey().Serialize(), nil, []byte("p"))
	require.ErrorContains(t, err, "empty tag")
}
