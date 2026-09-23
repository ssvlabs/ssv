package validation

import (
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
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

// A signer's slot-round state tracks distinct BuilderRequestAuth signing roots independently of the §5
// preference roots: recording is idempotent per root, the two sets never bleed into each other's budgets.
func TestSlotRoundState_RequestAuthRoots(t *testing.T) {
	s := &SignerStateForSlotRound{}
	r1 := [32]byte{1}
	r2 := [32]byte{2}

	require.Empty(t, s.SeenRequestAuthRoots)
	require.False(t, s.SeenRequestAuthRoots.has(r1))

	s.SeenRequestAuthRoots.record(r1)
	require.True(t, s.SeenRequestAuthRoots.has(r1))
	require.Len(t, s.SeenRequestAuthRoots, 1)

	// Recording an already-seen root is a no-op.
	s.SeenRequestAuthRoots.record(r1)
	require.Len(t, s.SeenRequestAuthRoots, 1)

	s.SeenRequestAuthRoots.record(r2)
	require.Len(t, s.SeenRequestAuthRoots, 2)

	// The two root sets are independent: the same root counts once per type, not globally.
	s.SeenProposerPreferencesRoots.record(r1)
	require.Len(t, s.SeenProposerPreferencesRoots, 1)
	require.Len(t, s.SeenRequestAuthRoots, 2)
}

// RequestAuth pre-consensus admits up to maxRequestAuthDistinctRoots distinct signing roots per
// (slot, signer) — one per configured builder (issue #2962) — with the §5 dedup: a repeat of a
// recorded root, whichever peer relays it, and a distinct root past the cap are both IGNORE'd
// (SIP #94 §7). The budget is separate from the §5 preference budget.
func TestValidatePartialSignatureMessageLimit_RequestAuth(t *testing.T) {
	raMsg := func(root [32]byte) *spectypes.PartialSignatureMessages {
		return &spectypes.PartialSignatureMessages{
			Type:     spectypes.RequestAuthPartialSig,
			Slot:     1,
			Messages: []*spectypes.PartialSignatureMessage{{SigningRoot: root}},
		}
	}
	record := func(ss *SignerStateForSlotRound, root [32]byte) {
		ss.SeenRequestAuthRoots.record(root) // as updatePartialSignatureState records on ACCEPT
	}
	root := func(b byte) [32]byte { return [32]byte{b} }

	const peerA = peer.ID("A")
	const peerB = peer.ID("B")

	t.Run("distinct roots accepted up to the bound, then further distinct roots are ignored", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		for i := 0; i < maxRequestAuthDistinctRoots; i++ {
			r := root(byte(i + 1))
			require.NoError(t, validatePartialSignatureMessageLimit(raMsg(r), peerA, ss))
			record(ss, r)
		}

		var valErr Error
		err := validatePartialSignatureMessageLimit(raMsg(root(99)), peerA, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	})

	// Honest re-triggers reproduce identical auth roots by design, so the IGNORE rationale is strictly
	// stronger here than for the preference roots.
	t.Run("a repeated root is ignored whichever peer relays it", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		r := root(1)
		require.NoError(t, validatePartialSignatureMessageLimit(raMsg(r), peerA, ss))
		record(ss, r)

		for _, from := range []peer.ID{peerA, peerB} {
			var valErr Error
			err := validatePartialSignatureMessageLimit(raMsg(r), from, ss)
			require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
			require.True(t, errors.As(err, &valErr))
			require.False(t, valErr.reject, "peer %s", from)
		}
	})

	t.Run("§5 preference roots do not consume the request-auth budget (and vice versa)", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		for i := 0; i < maxProposerPreferencesDistinctRoots; i++ {
			ss.SeenProposerPreferencesRoots.record(root(byte(100 + i)))
		}
		// The §5 budget is spent; a request-auth root is still admitted.
		require.NoError(t, validatePartialSignatureMessageLimit(raMsg(root(1)), peerA, ss))
		record(ss, root(1))
		// And the request-auth root did not consume the §5 budget's tracking.
		require.Len(t, ss.SeenRequestAuthRoots, 1)
		require.Len(t, ss.SeenProposerPreferencesRoots, maxProposerPreferencesDistinctRoots)
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

// A request-auth packet may batch several roots (SIP #94 §5/§7). The budget counts distinct roots: a batch is
// IGNOREd when it adds nothing or would take the signer past 8 roots, and otherwise passes even when it mixes
// recorded and new roots. A root repeated within a packet counts once.
func TestValidateDistinctRootBudget_RequestAuthBatches(t *testing.T) {
	root := func(b byte) [32]byte { return [32]byte{b} }
	batch := func(roots ...[32]byte) *spectypes.PartialSignatureMessages {
		m := &spectypes.PartialSignatureMessages{Type: spectypes.RequestAuthPartialSig, Slot: 1}
		for _, r := range roots {
			m.Messages = append(m.Messages, &spectypes.PartialSignatureMessage{SigningRoot: r})
		}
		return m
	}
	recorded := func(roots ...[32]byte) *SignerStateForSlotRound {
		ss := newSignerState(1, specqbft.FirstRound)
		for _, r := range roots {
			ss.SeenRequestAuthRoots.record(r)
		}
		return ss
	}
	requireIgnored := func(t *testing.T, err error) {
		t.Helper()
		var valErr Error
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	}
	const from = peer.ID("A")

	eight := make([][32]byte, 0, 8)
	for i := byte(1); i <= 8; i++ {
		eight = append(eight, root(i))
	}
	require.NoError(t, validatePartialSignatureMessageLimit(batch(eight...), from, recorded()), "a full batch of new roots")
	requireIgnored(t, validatePartialSignatureMessageLimit(batch(root(9)), from, recorded(eight...)))

	requireIgnored(t, validatePartialSignatureMessageLimit(batch(root(1), root(2)), from, recorded(root(1), root(2))))
	require.NoError(t, validatePartialSignatureMessageLimit(batch(root(1), root(3)), from, recorded(root(1), root(2))), "recorded and new mixed")

	six := eight[:6]
	requireIgnored(t, validatePartialSignatureMessageLimit(batch(root(7), root(8), root(9)), from, recorded(six...)))
	require.NoError(t, validatePartialSignatureMessageLimit(batch(root(7), root(7), root(8)), from, recorded(six...)),
		"a root repeated within the packet counts once")
}

// Through the whole validation of a signed message: a batch of 8 auth entries is accepted and records every
// root; 9 entries are rejected before the budget sees them; a batch mixing signers or validator indices is
// rejected.
func TestValidatePartialSignatureMessage_RequestAuthBatch(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	mv := &messageValidator{netCfg: netCfg}
	slot := netCfg.FirstSlotAtEpoch(gloasEpoch)

	packet := func(n int, index func(i int) phase0.ValidatorIndex) *spectypes.PartialSignatureMessages {
		m := &spectypes.PartialSignatureMessages{Type: spectypes.RequestAuthPartialSig, Slot: slot}
		for i := 0; i < n; i++ {
			m.Messages = append(m.Messages, &spectypes.PartialSignatureMessage{Signer: 1, ValidatorIndex: index(i), SigningRoot: [32]byte{byte(i + 1)}})
		}
		return m
	}
	same := func(int) phase0.ValidatorIndex { return 1 }
	signed := &spectypes.SignedSSVMessage{
		OperatorIDs: []spectypes.OperatorID{1},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   ssvtestingutils.NewMsgID(netCfg.DomainType, make([]byte, 48), spectypes.RoleProposerPreferences),
		},
	}
	indices := []phase0.ValidatorIndex{1}

	require.NoError(t, mv.validatePartialSignatureMessageSemantics(signed, packet(8, same), indices))

	var valErr Error
	err := mv.validatePartialSignatureMessageSemantics(signed, packet(9, same), indices)
	require.ErrorIs(t, err, ErrTooManySignaturesInPartialSigMessage)
	require.True(t, errors.As(err, &valErr))
	require.True(t, valErr.reject)

	mixed := packet(2, func(i int) phase0.ValidatorIndex { return phase0.ValidatorIndex(i + 1) })
	require.ErrorIs(t, mv.validatePartialSignatureMessageSemantics(signed, mixed, []phase0.ValidatorIndex{1, 2}), ErrInconsistentValidatorIndex)
	mixedSigners := packet(2, same)
	mixedSigners.Messages[1].Signer = 2
	require.ErrorIs(t, mv.validatePartialSignatureMessageSemantics(signed, mixedSigners, indices), ErrInconsistentSigners)

	// An accepted batch records all of its roots together.
	ci := newCommitteeInfo(spectypes.CommitteeID{}, []spectypes.OperatorID{1, 2, 3, 4}, indices, 0, 0)
	state := &ValidatorState{operators: make([]*OperatorState, 4), storedSlotCount: mv.storedSlotCount(spectypes.RoleProposerPreferences), storedEpochCount: mv.storedEpochCount(spectypes.RoleProposerPreferences)}
	require.NoError(t, mv.updatePartialSignatureState(packet(8, same), "", state, 1, ci))
	require.Len(t, state.OperatorState(0).GetSignerStateForSlot(slot).SeenRequestAuthRoots, 8)
}
