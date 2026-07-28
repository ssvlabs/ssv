package validation

import (
	"errors"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// RequestAuth partials ride the RoleProposerPreferences wire (issue #2962): the role admits both
// partial-sig types, and no other role admits RequestAuthPartialSig.
func TestPartialSignatureTypeMatchesRole_RequestAuth(t *testing.T) {
	mv := &messageValidator{}
	require.True(t, mv.partialSignatureTypeMatchesRole(spectypes.RequestAuthPartialSig, spectypes.RoleProposerPreferences))
	require.False(t, mv.partialSignatureTypeMatchesRole(spectypes.RequestAuthPartialSig, spectypes.RoleProposer))
	require.False(t, mv.partialSignatureTypeMatchesRole(spectypes.RequestAuthPartialSig, spectypes.RoleValidatorRegistration))
}

func TestValidPartialSigMsgType_RequestAuth(t *testing.T) {
	mv := &messageValidator{}
	require.True(t, mv.validPartialSigMsgType(spectypes.RequestAuthPartialSig))
}

// SignerState tracks distinct RequestAuthV1 signing roots independently of the §5 preference roots:
// recording is idempotent per root, the two sets never bleed into each other's budgets.
func TestSignerState_RequestAuthRoots(t *testing.T) {
	s := &SignerState{}
	r1 := [32]byte{1}
	r2 := [32]byte{2}

	require.Equal(t, 0, s.requestAuthRootCount())
	require.False(t, s.hasRequestAuthRoot(r1))

	s.recordRequestAuthRoot(r1)
	require.True(t, s.hasRequestAuthRoot(r1))
	require.Equal(t, 1, s.requestAuthRootCount())

	// Recording an already-seen root is a no-op.
	s.recordRequestAuthRoot(r1)
	require.Equal(t, 1, s.requestAuthRootCount())

	s.recordRequestAuthRoot(r2)
	require.Equal(t, 2, s.requestAuthRootCount())

	// The two root sets are independent: the same root counts once per type, not globally.
	s.recordProposerPreferencesRoot(r1)
	require.Equal(t, 1, s.proposerPreferencesRootCount())
	require.Equal(t, 2, s.requestAuthRootCount())
}

// RequestAuth pre-consensus admits up to maxRequestAuthDistinctRoots distinct signing roots per
// (slot, signer) — one per configured builder (issue #2962) — with the §5 two-tier handling: only a
// same-peer repeat of a seen root is REJECT'd; a relayed repeat or a distinct root past the cap is
// IGNORE'd. The budget is separate from the §5 preference budget.
func TestValidatePartialSignatureMessageLimit_RequestAuth(t *testing.T) {
	raMsg := func(root [32]byte) *spectypes.PartialSignatureMessages {
		return &spectypes.PartialSignatureMessages{
			Type:     spectypes.RequestAuthPartialSig,
			Slot:     1,
			Messages: []*spectypes.PartialSignatureMessage{{SigningRoot: root}},
		}
	}
	record := func(ss *SignerStateForSlotRound, from peer.ID, root [32]byte) {
		ss.Peer(from).recordRequestAuthRoot(root)
		ss.World.recordRequestAuthRoot(root)
	}
	root := func(b byte) [32]byte { return [32]byte{b} }

	const peerA = peer.ID("A")
	const peerB = peer.ID("B")

	t.Run("distinct roots accepted up to the bound, then further distinct roots are ignored", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		for i := 0; i < maxRequestAuthDistinctRoots; i++ {
			r := root(byte(i + 1))
			require.NoError(t, validatePartialSignatureMessageLimit(raMsg(r), peerA, ss))
			record(ss, peerA, r)
		}

		var valErr Error
		err := validatePartialSignatureMessageLimit(raMsg(root(99)), peerA, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	})

	t.Run("same-peer duplicate root is rejected, a relayed duplicate is ignored", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		r := root(1)
		require.NoError(t, validatePartialSignatureMessageLimit(raMsg(r), peerA, ss))
		record(ss, peerA, r)

		var valErr Error
		err := validatePartialSignatureMessageLimit(raMsg(r), peerA, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.True(t, valErr.reject)

		err = validatePartialSignatureMessageLimit(raMsg(r), peerB, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	})

	t.Run("§5 preference roots do not consume the request-auth budget (and vice versa)", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		for i := 0; i < maxProposerPreferencesDistinctRoots; i++ {
			ss.Peer(peerA).recordProposerPreferencesRoot(root(byte(100 + i)))
			ss.World.recordProposerPreferencesRoot(root(byte(100 + i)))
		}
		// The §5 budget is spent; a request-auth root is still admitted.
		require.NoError(t, validatePartialSignatureMessageLimit(raMsg(root(1)), peerA, ss))
		record(ss, peerA, root(1))
		// And the request-auth root did not consume the §5 budget's tracking.
		require.Equal(t, 1, ss.World.requestAuthRootCount())
		require.Equal(t, maxProposerPreferencesDistinctRoots, ss.World.proposerPreferencesRootCount())
	})
}

// RequestAuthPartialSig, like the §5 preference type, is budgeted by distinct root — recording it
// must not consume the single pre-consensus bit in SeenMsgTypes that caps every other
// pre-consensus type at one message.
func TestSeenMsgTypes_RequestAuthDoesNotConsumePreConsensusBit(t *testing.T) {
	var seen SeenMsgTypes
	require.NoError(t, seen.RecordPartialSignatureMessage(&spectypes.PartialSignatureMessages{Type: spectypes.RequestAuthPartialSig}))
	require.False(t, seen.reachedPreConsensusLimit())

	require.NoError(t, seen.RecordPartialSignatureMessage(&spectypes.PartialSignatureMessages{Type: spectypes.PTCAttesterPartialSig}))
	require.True(t, seen.reachedPreConsensusLimit())
}
