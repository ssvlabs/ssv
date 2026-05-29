package blsbackend

import (
	"fmt"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// Tests for audit finding F18: BLSSigner.AggregatePartials sets the Shamir
// x-coordinate bls.ID via SetLittleEndian(opID) instead of the prior
// SetDecString(fmt.Sprintf("%d", opID)) string round-trip. These pin the
// equivalence the optimisation relies on.

// TestBLSSigner_AggregateID_LittleEndianEqualsDecString — for the operator-ID
// range OBFT uses (1-indexed cluster positions, well under any realistic
// cluster size), the little-endian-bytes encoding of opID produces the SAME
// bls.ID as the old decimal-string encoding. If this ever diverged, Lagrange
// interpolation in Recover would use the wrong x-coordinates and reconstruct
// a wrong signature — so it's the load-bearing equivalence for F18.
func TestBLSSigner_AggregateID_LittleEndianEqualsDecString(t *testing.T) {
	threshold.Init()
	for _, opID := range []uint64{1, 2, 3, 4, 7, 13, 100, 255, 256, 65535, 1 << 20} {
		var leID bls.ID
		var leBuf [8]byte
		// little-endian, matching AggregatePartials.
		for i := 0; i < 8; i++ {
			leBuf[i] = byte(opID >> (8 * i))
		}
		require.NoError(t, leID.SetLittleEndian(leBuf[:]), "SetLittleEndian opID=%d", opID)

		var decID bls.ID
		require.NoError(t, decID.SetDecString(fmt.Sprintf("%d", opID)), "SetDecString opID=%d", opID)

		require.Equal(t, decID.SerializeToHexStr(), leID.SerializeToHexStr(),
			"opID=%d: little-endian and decimal-string bls.ID encodings must match", opID)
	}
}

// TestBLSSigner_AggregatePartials_RoundTrips — end-to-end sanity that
// AggregatePartials (now using the little-endian ID encoding) reconstructs a
// signature that verifies against the master pubkey. This is the property
// that would break if the x-coordinate encoding were wrong.
func TestBLSSigner_AggregatePartials_RoundTrips(t *testing.T) {
	threshold.Init()

	const n, q = 4, 3 // 4 operators, threshold 3 (= 2f+1 at f=1)
	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey()

	// threshold.Create Shamir-splits the master into n shares keyed by
	// operator ID (1..n), recoverable from any q of them — the same helper
	// the rest of the blsbackend tests use (e.g. kyber_conversion_test.go).
	shares, err := threshold.Create(master.Serialize(), q, n)
	require.NoError(t, err)

	msg := []byte("the-message-the-cluster-signs--padded-to-32b!!!!")[:32]

	signer := New(nil) // verify/aggregate-only is all we need here
	partials := make(map[obft.OperatorID]obft.Signature, q)
	chosen := []uint64{1, 3, 4} // any q-of-n subset
	for _, op := range chosen {
		sig := shares[op].SignByte(msg)
		partials[obft.OperatorID(op)] = obft.Signature(sig.Serialize())
	}

	full, err := signer.AggregatePartials(partials)
	require.NoError(t, err)

	var recovered bls.Sign
	require.NoError(t, recovered.Deserialize(full))
	require.True(t, recovered.VerifyByte(masterPub, msg),
		"aggregate of any k-of-n subset must verify against the master pubkey")
}
