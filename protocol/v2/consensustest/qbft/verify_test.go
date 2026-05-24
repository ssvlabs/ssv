package qbft

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestCachingVerifierMatchesSpecVerify pins the safety contract of the
// memoizing verifier: it must return exactly what spectypes.Verify would, for
// valid AND rejected signatures, on both the cache-miss and cache-hit paths —
// and the per-n key must stop one cluster size's verdict from leaking to
// another (whose keyset, hence operator 1's key, differs). A regression here
// would let the stress sweep accept signatures the real protocol rejects,
// silently weakening the safety checks the sweep exists to exercise.
func TestCachingVerifierMatchesSpecVerify(t *testing.T) {
	ks4, err := keysetForN(4)
	require.NoError(t, err)
	cm4, err := committeeForN(4)
	require.NoError(t, err)

	// Validly-signed PROPOSE from operator 1 under the n=4 keyset.
	msg, err := makeProposalEnvelope(1, ks4.OperatorKeys[1], stableIdentifier(),
		specqbft.FirstHeight, specqbft.FirstRound, []byte("test-value"))
	require.NoError(t, err)

	v4 := newCachingVerifier(4)

	// Valid signature: spec accepts, so the cache must accept on both the
	// first (miss → delegates to spectypes.Verify) and second (hit) call.
	require.NoError(t, spectypes.Verify(msg, cm4.Committee))
	require.NoError(t, v4(msg, cm4.Committee), "valid, miss")
	require.NoError(t, v4(msg, cm4.Committee), "valid, hit")

	// Tampered signature: spec rejects, so the cache must reject identically on
	// both miss and hit (the rejection is keyed on the distinct signature bytes,
	// so it never collides with the accepted entry above).
	bad := *msg
	bad.Signatures = [][]byte{append([]byte(nil), msg.Signatures[0]...)}
	bad.Signatures[0][0] ^= 0xFF
	require.Error(t, spectypes.Verify(&bad, cm4.Committee))
	require.Error(t, v4(&bad, cm4.Committee), "tampered, miss")
	require.Error(t, v4(&bad, cm4.Committee), "tampered, hit")

	// Cross-n isolation: the n=4 verifier cached msg as valid, but operator 1
	// holds a different key under the n=7 keyset, so a verifier for n=7 must
	// recompute (its per-n key differs) and reject, matching spectypes.Verify.
	cm7, err := committeeForN(7)
	require.NoError(t, err)
	v7 := newCachingVerifier(7)
	require.Error(t, spectypes.Verify(msg, cm7.Committee))
	require.Error(t, v7(msg, cm7.Committee), "n=7 must not reuse the n=4 verdict")
}
