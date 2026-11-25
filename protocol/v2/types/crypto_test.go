package types

import (
	"fmt"
	"slices"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestVerifyBeaconPartialSignature(t *testing.T) {
	initBLS(t)

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()
	pk := sk.GetPublicKey().Serialize()

	const signer = spectypes.OperatorID(3)
	committee := []*spectypes.ShareMember{{Signer: signer, SharePubKey: pk}}

	var root [32]byte
	copy(root[:], "the-root-we-expect-to-be-signed.")
	goodSig := sk.SignByte(root[:]).Serialize()

	// a correct signature over the expected root, by a committee member, verifies
	require.NoError(t, VerifyBeaconPartialSignature(signer, goodSig, root, committee))

	// a signature over a different root (the wrong-domain / misconfigured-signer case) is rejected
	var otherRoot [32]byte
	copy(otherRoot[:], "a-different-root-the-wrong-domain")
	require.Error(t, VerifyBeaconPartialSignature(signer, sk.SignByte(otherRoot[:]).Serialize(), root, committee))

	// a signer not in the committee is rejected
	require.Error(t, VerifyBeaconPartialSignature(spectypes.OperatorID(99), goodSig, root, committee))
}

func TestValidateSharePublicKeys(t *testing.T) {
	initBLS(t)

	committees := map[string][]uint64{
		"4 operators":  {1, 2, 3, 4},
		"7 operators":  {5, 17, 23, 42, 100, 101, 999},
		"10 operators": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		"13 operators": {11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
	}

	for name, ids := range committees {
		quorum := quorumSize(len(ids))

		t.Run(name+"/valid", func(t *testing.T) {
			validatorPK, sharePKs := splitKey(t, ids, quorum-1)
			require.NoError(t, ValidateSharePublicKeys(validatorPK, ids, sharePKs))
		})

		t.Run(name+"/different validator key", func(t *testing.T) {
			_, sharePKs := splitKey(t, ids, quorum-1)
			otherPK, _ := splitKey(t, ids, quorum-1)
			require.ErrorIs(t, ValidateSharePublicKeys(otherPK, ids, sharePKs), ErrInvalidSharePublicKeys)
		})

		t.Run(name+"/one share replaced", func(t *testing.T) {
			for i := range ids {
				validatorPK, sharePKs := splitKey(t, ids, quorum-1)
				_, otherSharePKs := splitKey(t, ids, quorum-1)
				sharePKs[i] = otherSharePKs[i]
				require.Error(t, ValidateSharePublicKeys(validatorPK, ids, sharePKs), "replaced share %d", i)
			}
		})

		t.Run(name+"/shares evaluated at wrong ids", func(t *testing.T) {
			wrongIDs := make([]uint64, len(ids))
			for i, id := range ids {
				wrongIDs[i] = id + 1000
			}
			validatorPK, sharePKs := splitKey(t, wrongIDs, quorum-1)
			require.Error(t, ValidateSharePublicKeys(validatorPK, ids, sharePKs))
		})

		t.Run(name+"/polynomial degree too low", func(t *testing.T) {
			for degree := 0; degree < quorum-1; degree++ {
				validatorPK, sharePKs := splitKey(t, ids, degree)
				err := ValidateSharePublicKeys(validatorPK, ids, sharePKs)
				require.ErrorIs(t, err, ErrInvalidSharePublicKeys, "degree %d", degree)
				require.ErrorContains(t, err, "degree below", "degree %d must be rejected", degree)
			}
		})

		t.Run(name+"/polynomial degree too high", func(t *testing.T) {
			validatorPK, sharePKs := splitKey(t, ids, quorum)
			require.Error(t, ValidateSharePublicKeys(validatorPK, ids, sharePKs))
		})

		t.Run(name+"/shares out of order", func(t *testing.T) {
			validatorPK, sharePKs := splitKey(t, ids, quorum-1)
			sharePKs[0], sharePKs[len(sharePKs)-1] = sharePKs[len(sharePKs)-1], sharePKs[0]
			require.Error(t, ValidateSharePublicKeys(validatorPK, ids, sharePKs))
		})
	}

	t.Run("invalid share public key bytes", func(t *testing.T) {
		ids := []uint64{1, 2, 3, 4}
		validatorPK, sharePKs := splitKey(t, ids, 2)
		sharePKs[1] = make([]byte, len(sharePKs[1]))
		require.ErrorIs(t, ValidateSharePublicKeys(validatorPK, ids, sharePKs), ErrInvalidSharePublicKeys)
	})

	t.Run("invalid committee size", func(t *testing.T) {
		for _, n := range []int{0, 1, 2, 3, 5, 6, 14, 16} {
			ids := make([]uint64, n)
			for i := range ids {
				ids[i] = uint64(i + 1)
			}
			// Shares are evaluated on a polynomial of the would-be quorum degree (0 for an empty set).
			degree := max(2*((n-1)/3), 0)
			validatorPK, sharePKs := splitKey(t, ids, degree)
			err := ValidateSharePublicKeys(validatorPK, ids, sharePKs)
			require.ErrorIs(t, err, ErrInvalidSharePublicKeys, "size %d", n)
			require.ErrorContains(t, err, "invalid committee size", "size %d must be rejected", n)
		}

		// A single-member committee whose share is the validator key itself.
		validatorPK, _ := splitKey(t, []uint64{1}, 0)
		err := ValidateSharePublicKeys(validatorPK, []uint64{1}, [][]byte{validatorPK})
		require.ErrorIs(t, err, ErrInvalidSharePublicKeys)
		require.ErrorContains(t, err, "invalid committee size")
	})

	t.Run("mismatched lengths", func(t *testing.T) {
		ids := []uint64{1, 2, 3, 4}
		validatorPK, sharePKs := splitKey(t, ids, 2)
		require.ErrorIs(t, ValidateSharePublicKeys(validatorPK, ids, sharePKs[:3]), ErrInvalidSharePublicKeys)
	})
}

func TestValidateSharePublicKeysRejectsIdentityKeys(t *testing.T) {
	initBLS(t)

	identity := append([]byte{0xc0}, make([]byte, 47)...)
	ids4 := []uint64{1, 2, 3, 4}
	ids7 := []uint64{1, 2, 3, 4, 5, 6, 7}

	pubKeys := func(validatorSK *bls.SecretKey, shareSKs []*bls.SecretKey) ([]byte, [][]byte) {
		sharePKs := make([][]byte, len(shareSKs))
		for i, sk := range shareSKs {
			sharePKs[i] = sk.GetPublicKey().Serialize()
		}
		return validatorSK.GetPublicKey().Serialize(), sharePKs
	}

	t.Run("control: no zero share", func(t *testing.T) {
		validatorPK, sharePKs := pubKeys(zeroRootSplit(t, ids4, 2))
		require.NoError(t, ValidateSharePublicKeys(validatorPK, ids4, sharePKs))
	})

	tests := []struct {
		name   string
		ids    []uint64
		degree int
		zeroAt []uint64
	}{
		{name: "one zero share", ids: ids4, degree: 2, zeroAt: []uint64{3}},
		{name: "two zero shares", ids: ids4, degree: 2, zeroAt: []uint64{2, 3}},
		{name: "zero share in base", ids: ids4, degree: 2, zeroAt: []uint64{1}},
		{name: "identity validator key", ids: ids4, degree: 2, zeroAt: []uint64{0}},
		{name: "7 operators, two zero shares", ids: ids7, degree: 4, zeroAt: []uint64{5, 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validatorPK, sharePKs := pubKeys(zeroRootSplit(t, tt.ids, tt.degree, tt.zeroAt...))
			for _, z := range tt.zeroAt {
				if z == 0 {
					require.Equal(t, identity, validatorPK)
					continue
				}
				require.Equal(t, identity, sharePKs[slices.Index(tt.ids, z)], "share of operator %d", z)
			}

			err := ValidateSharePublicKeys(validatorPK, tt.ids, sharePKs)
			require.ErrorIs(t, err, ErrInvalidSharePublicKeys)
			require.ErrorContains(t, err, "identity point")
		})
	}

	// The check must run on the decoded point, not on the bytes: the BLS library also decodes
	// non-canonical infinity encodings (infinity flag set, non-zero trailing bytes) to the identity.
	t.Run("non-canonical identity encoding", func(t *testing.T) {
		nonCanonical := slices.Clone(identity)
		nonCanonical[len(nonCanonical)-1] = 0x01
		var pk bls.PublicKey
		require.NoError(t, pk.Deserialize(nonCanonical))
		require.True(t, pk.IsZero())

		validatorPK, sharePKs := pubKeys(zeroRootSplit(t, ids4, 2, 3))
		sharePKs[slices.Index(ids4, 3)] = nonCanonical

		err := ValidateSharePublicKeys(validatorPK, ids4, sharePKs)
		require.ErrorIs(t, err, ErrInvalidSharePublicKeys)
		require.ErrorContains(t, err, "identity point")
	})

	t.Run("non-canonical identity validator key", func(t *testing.T) {
		nonCanonical := slices.Clone(identity)
		nonCanonical[len(nonCanonical)-1] = 0x01

		// Shares of a polynomial with f(0) = 0 reconstruct the identity, so only the identity
		// check on the decoded validator key can catch this.
		_, sharePKs := pubKeys(zeroRootSplit(t, ids4, 2, 0))

		err := ValidateSharePublicKeys(nonCanonical, ids4, sharePKs)
		require.ErrorIs(t, err, ErrInvalidSharePublicKeys)
		require.ErrorContains(t, err, "validator public key is the identity point")
	})
}

// quorumSize returns the quorum of a committee of n operators.
func quorumSize(n int) int {
	quorum, _ := ComputeQuorumAndPartialQuorum(uint64(n))
	return int(quorum)
}

func initBLS(tb testing.TB) {
	require.NoError(tb, bls.Init(bls.BLS12_381))
	require.NoError(tb, bls.SetETHmode(bls.EthModeDraft07))
}

// splitKey splits a fresh validator key into shares evaluated at the given IDs
// using a random polynomial of degree polyDegree.
func splitKey(tb testing.TB, ids []uint64, polyDegree int) ([]byte, [][]byte) {
	msk := make([]bls.SecretKey, polyDegree+1)
	for i := range msk {
		msk[i].SetByCSPRNG()
	}

	sharePubKeys := make([][]byte, len(ids))
	for i, id := range ids {
		var blsID bls.ID
		require.NoError(tb, blsID.SetDecString(fmt.Sprintf("%d", id)))

		var share bls.SecretKey
		require.NoError(tb, share.Set(msk, &blsID))
		sharePubKeys[i] = share.GetPublicKey().Serialize()
	}

	return msk[0].GetPublicKey().Serialize(), sharePubKeys
}

// zeroRootSplit evaluates f = g * prod(x - z) over zeroAt, with g random of degree
// degree-len(zeroAt): f has degree exactly `degree` and f(z) = 0. Returns f(0) and f(id) per id.
func zeroRootSplit(tb testing.TB, ids []uint64, degree int, zeroAt ...uint64) (*bls.SecretKey, []*bls.SecretKey) {
	g := make([]bls.Fr, degree-len(zeroAt)+1)
	for i := range g {
		g[i].SetByCSPRNG()
	}
	eval := func(x uint64) *bls.SecretKey {
		var fx, acc bls.Fr
		fx.SetInt64(int64(x))
		for i := len(g) - 1; i >= 0; i-- { // Horner
			bls.FrMul(&acc, &acc, &fx)
			bls.FrAdd(&acc, &acc, &g[i])
		}
		for _, z := range zeroAt {
			var fz, d bls.Fr
			fz.SetInt64(int64(z))
			bls.FrSub(&d, &fx, &fz)
			bls.FrMul(&acc, &acc, &d)
		}
		return bls.CastToSecretKey(&acc)
	}
	shares := make([]*bls.SecretKey, len(ids))
	for i, id := range ids {
		shares[i] = eval(id)
	}
	return eval(0), shares
}

func BenchmarkValidateSharePublicKeys(b *testing.B) {
	initBLS(b)

	for _, n := range []int{4, 7, 10, 13} {
		ids := make([]uint64, n)
		for i := range ids {
			ids[i] = uint64(i + 1)
		}
		quorum := quorumSize(n)
		validatorPK, sharePKs := splitKey(b, ids, quorum-1)

		b.Run(fmt.Sprintf("%d operators", n), func(b *testing.B) {
			for b.Loop() {
				if err := ValidateSharePublicKeys(validatorPK, ids, sharePKs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
