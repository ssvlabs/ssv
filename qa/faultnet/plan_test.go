package faultnet

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/qa/faults"
)

// partialMsg builds a one-entry partial-signature message for the given role and slot, the shape
// every wire fault starts from.
func partialMsg(t *testing.T, role spectypes.RunnerRole, slot phase0.Slot) *spectypes.SignedSSVMessage {
	t.Helper()

	var pk spectypes.ValidatorPK
	pk[0] = 0xab

	body := &spectypes.PartialSignatureMessages{
		Type: spectypes.PTCAttesterPartialSig,
		Slot: slot,
		Messages: []*spectypes.PartialSignatureMessage{{
			PartialSignature: make([]byte, 96), // spectypes.Signature is []byte, ssz-size 96
			SigningRoot:      [32]byte{0x01},
			Signer:           7,
			ValidatorIndex:   42,
		}},
	}
	data, err := body.Encode()
	require.NoError(t, err)

	return &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{{0x09}},
		OperatorIDs: []spectypes.OperatorID{7},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewValidatorMsgID(spectypes.DomainType{0x00, 0x00, 0x31, 0x14}, pk, role),
			Data:    data,
		},
	}
}

func TestPlanPassesThroughWhenNoFault(t *testing.T) {
	msg := partialMsg(t, spectypes.RolePTCAttester, 100)

	out := Plan(faults.None, msg, 100, 100)

	require.Len(t, out, 1)
	require.Same(t, msg, out[0].Msg, "the identity path must not copy the message")
	require.False(t, out[0].Resign)
	require.Zero(t, out[0].Delay)
	require.Equal(t, phase0.Slot(100), out[0].Slot)
}

func TestPlanLeavesUnrelatedMessagesAlone(t *testing.T) {
	// two-entries targets role 7 partials; a role-0 committee message must pass straight through.
	msg := partialMsg(t, spectypes.RoleCommittee, 100)

	out := Plan(faults.TwoEntries, msg, 100, 100)

	require.Len(t, out, 1)
	require.Same(t, msg, out[0].Msg)
	require.False(t, out[0].Resign)
}

func TestCloneIsIndependent(t *testing.T) {
	msg := partialMsg(t, spectypes.RolePTCAttester, 100)

	c, err := Clone(msg)
	require.NoError(t, err)
	require.NotSame(t, msg, c)

	c.SSVMessage.Data[0] ^= 0xff
	require.NotEqual(t, msg.SSVMessage.Data[0], c.SSVMessage.Data[0])
}

func prefsMsg(t *testing.T, slot phase0.Slot) *spectypes.SignedSSVMessage {
	t.Helper()
	msg := partialMsg(t, spectypes.RoleProposerPreferences, slot)
	body := &spectypes.PartialSignatureMessages{}
	require.NoError(t, body.Decode(msg.SSVMessage.Data))
	body.Type = spectypes.ProposerPreferencesPartialSig
	data, err := body.Encode()
	require.NoError(t, err)
	msg.SSVMessage.Data = data
	return msg
}

func decodePartial(t *testing.T, msg *spectypes.SignedSSVMessage) *spectypes.PartialSignatureMessages {
	t.Helper()
	body := &spectypes.PartialSignatureMessages{}
	require.NoError(t, body.Decode(msg.SSVMessage.Data))
	return body
}

func TestPlanPrefs5Roots(t *testing.T) {
	msg := prefsMsg(t, 200)

	out := Plan(faults.Prefs5Roots, msg, 200, 150)

	require.Len(t, out, 5, "the honest message plus four extra roots")
	require.Same(t, msg, out[0].Msg)
	require.False(t, out[0].Resign)

	seen := map[[32]byte]bool{decodePartial(t, msg).Messages[0].SigningRoot: true}
	for _, o := range out[1:] {
		require.True(t, o.Resign)
		root := decodePartial(t, o.Msg).Messages[0].SigningRoot
		require.False(t, seen[root], "each extra message must carry a distinct root")
		seen[root] = true
	}
	require.Len(t, seen, 5)
}

func TestPlanPrefsEarlyAndLate(t *testing.T) {
	t.Run("early is 65 slots ahead of now", func(t *testing.T) {
		msg := prefsMsg(t, 200)
		out := Plan(faults.PrefsEarly, msg, 200, 150)
		require.Len(t, out, 2)
		require.Same(t, msg, out[0].Msg)
		require.True(t, out[1].Resign)
		require.Equal(t, phase0.Slot(215), decodePartial(t, out[1].Msg).Slot)
		require.Equal(t, phase0.Slot(200), out[1].Slot, "the topic must follow the original slot")
	})

	t.Run("late is 3 slots behind now", func(t *testing.T) {
		msg := prefsMsg(t, 200)
		out := Plan(faults.PrefsLate, msg, 200, 150)
		require.Len(t, out, 2)
		require.Equal(t, phase0.Slot(147), decodePartial(t, out[1].Msg).Slot)
	})

	t.Run("late does not underflow near genesis", func(t *testing.T) {
		msg := prefsMsg(t, 2)
		out := Plan(faults.PrefsLate, msg, 2, 1)
		require.Len(t, out, 1, "no room to be late yet, send only the honest message")
	})
}

func TestPlanPrefsReplay(t *testing.T) {
	msg := prefsMsg(t, 200)
	honestRoot := decodePartial(t, msg).Messages[0].SigningRoot

	out := Plan(faults.PrefsReplay, msg, 200, 150)

	require.Len(t, out, 2)
	require.Same(t, msg, out[0].Msg)

	replay := out[1]
	require.True(t, replay.Resign)
	require.Equal(t, replayCount, replay.Repeat)
	require.Equal(t, replayEvery, replay.Every)
	body := decodePartial(t, replay.Msg)
	require.Equal(t, honestRoot, body.Messages[0].SigningRoot, "the replay must keep the signing root")
	require.NotEqual(t, decodePartial(t, msg).Messages[0].PartialSignature, body.Messages[0].PartialSignature,
		"the bytes must differ or gossipsub suppresses the duplicate")
}

func TestPlanPTCQBFT(t *testing.T) {
	msg := partialMsg(t, spectypes.RolePTCAttester, 200)

	out := Plan(faults.PTCQBFT, msg, 200, 200)

	require.Len(t, out, 2)
	require.Same(t, msg, out[0].Msg)

	forged := out[1]
	require.True(t, forged.Resign)
	require.Equal(t, spectypes.SSVConsensusMsgType, forged.Msg.SSVMessage.MsgType)
	require.Equal(t, spectypes.RolePTCAttester, forged.Msg.SSVMessage.GetID().GetRoleType())

	body := &specqbft.Message{}
	require.NoError(t, body.Decode(forged.Msg.SSVMessage.Data))
	require.Equal(t, specqbft.ProposalMsgType, body.MsgType)
	require.Equal(t, specqbft.Height(200), body.Height)
	require.Equal(t, specqbft.Round(1), body.Round)
}

func TestPlanTwoEntries(t *testing.T) {
	msg := partialMsg(t, spectypes.RolePTCAttester, 200)

	out := Plan(faults.TwoEntries, msg, 200, 200)

	// Only the forged clone is sent: sending the honest 1-entry message first would create signer
	// state for (signer, slot) that trips the pre-consensus message-limit rule before the entry-count
	// rule under test is ever reached. See dupEntry's doc comment.
	require.Len(t, out, 1)
	require.NotSame(t, msg, out[0].Msg, "must be a clone, not the original")
	require.Len(t, decodePartial(t, out[0].Msg).Messages, 2)
	require.True(t, out[0].Resign)
}

// The two forged copies below are refused by the §7 PTC assignment gate (ErrNoDuty), not by the
// per-epoch duty-count rule — a validator holds exactly one PTC duty slot per epoch, so that gate
// fires before the count is ever checked. Do not "fix" this back to backdated slots: a backdated
// copy dies on the monotonic-slot check instead, never reaching the assignment gate either. See
// ptcExtraSlots's doc comment in plan.go.
func TestPlanPTC3PerEpoch(t *testing.T) {
	t.Run("two extra slots inside the same epoch", func(t *testing.T) {
		// Slot 200 sits in epoch 6 (slots 192 to 223), so 201 and 202 are same-epoch.
		msg := partialMsg(t, spectypes.RolePTCAttester, 200)

		out := Plan(faults.PTC3PerEpoch, msg, 200, 200)

		require.Len(t, out, 3)
		require.Same(t, msg, out[0].Msg, "the honest message is untouched")
		require.False(t, out[0].Resign)

		require.Equal(t, phase0.Slot(201), decodePartial(t, out[1].Msg).Slot)
		require.Equal(t, 1, out[1].DelaySlots)
		require.Equal(t, phase0.Slot(202), decodePartial(t, out[2].Msg).Slot)
		require.Equal(t, 2, out[2].DelaySlots)
		for _, o := range out[1:] {
			require.True(t, o.Resign)
		}
	})

	t.Run("no room at the end of an epoch", func(t *testing.T) {
		// Slot 223 is the last slot of epoch 6 (192 to 223): neither 224 nor 225 is same-epoch, so
		// the fault has no forward room and falls back to the honest message alone.
		msg := partialMsg(t, spectypes.RolePTCAttester, 223)
		out := Plan(faults.PTC3PerEpoch, msg, 223, 223)
		require.Len(t, out, 1)
	})

	t.Run("room for only one extra slot", func(t *testing.T) {
		// Slot 222 has room for 223 (same epoch) but not 224 (next epoch): all-or-nothing means
		// neither extra copy is sent.
		msg := partialMsg(t, spectypes.RolePTCAttester, 222)
		out := Plan(faults.PTC3PerEpoch, msg, 222, 222)
		require.Len(t, out, 1)
	})
}

func TestPlanRole7PreFork(t *testing.T) {
	msg := partialMsg(t, spectypes.RoleValidatorRegistration, 100)

	out := Plan(faults.Role7PreFork, msg, 100, 100)

	require.Len(t, out, 4, "the honest registration plus roles 7, 8 and 9")
	roles := []spectypes.RunnerRole{
		out[1].Msg.SSVMessage.GetID().GetRoleType(),
		out[2].Msg.SSVMessage.GetID().GetRoleType(),
		out[3].Msg.SSVMessage.GetID().GetRoleType(),
	}
	require.ElementsMatch(t, []spectypes.RunnerRole{
		spectypes.RolePTCAttester, spectypes.RoleProposerPreferences, spectypes.RoleEnvelopeProposer,
	}, roles)
	for _, o := range out[1:] {
		require.True(t, o.Resign)
		require.Equal(t, phase0.Slot(100), decodePartial(t, o.Msg).Slot, "the forged messages keep the pre-fork slot")
	}
}
