package faultnet

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
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

	out := Plan(faults.None, msg, 100, 100, 0)

	require.Len(t, out, 1)
	require.Same(t, msg, out[0].Msg, "the identity path must not copy the message")
	require.False(t, out[0].Resign)
	require.Zero(t, out[0].Delay)
	require.Equal(t, phase0.Slot(100), out[0].Slot)
}

func TestPlanLeavesUnrelatedMessagesAlone(t *testing.T) {
	// two-entries targets role 7 partials; a role-0 committee message must pass straight through.
	msg := partialMsg(t, spectypes.RoleCommittee, 100)

	out := Plan(faults.TwoEntries, msg, 100, 100, 0)

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

	out := Plan(faults.Prefs5Roots, msg, 200, 150, 0)

	require.Len(t, out, 6, "the honest message, four extra roots, and one repeat of the honest root")
	require.Same(t, msg, out[0].Msg)
	require.False(t, out[0].Resign)

	honest := decodePartial(t, msg).Messages[0]
	seen := map[[32]byte]bool{honest.SigningRoot: true}
	for i, o := range out[1:5] {
		require.True(t, o.Resign)
		require.Equal(t, time.Duration(i+1)*100*time.Millisecond, o.Delay,
			"the clones are staggered so the honest root is always recorded first")
		root := decodePartial(t, o.Msg).Messages[0].SigningRoot
		require.False(t, seen[root], "each extra message must carry a distinct root")
		seen[root] = true
	}
	require.Len(t, seen, 5)

	// MSG-05 clause 3: a byte-distinct repeat of the accepted honest root, well after the fifth root.
	repeat := out[5]
	require.True(t, repeat.Resign)
	require.Equal(t, 2*time.Second, repeat.Delay)
	require.Equal(t, "repeat", repeat.Label)
	body := decodePartial(t, repeat.Msg).Messages[0]
	require.Equal(t, honest.SigningRoot, body.SigningRoot, "the repeat carries the honest, accepted root")
	require.NotEqual(t, honest.PartialSignature, body.PartialSignature,
		"the bytes must differ or the gossip seen-cache drops the repeat before validation")
	for _, o := range out {
		require.Equal(t, phase0.Slot(200), o.Slot, "the topic follows the original slot")
	}
}

// prefs-34-apart (MSG-06), addendum D.2: P = 357 is in epoch 11 (352..383) at k = 5; the honest
// emission tick now = 335 is slot 15 of epoch 10, so d0 = 352 - 335 = 17.
func TestPlanPrefs34Apart(t *testing.T) {
	const p, now = phase0.Slot(357), phase0.Slot(335)

	t.Run("fires the full sequence for a next-epoch proposal slot", func(t *testing.T) {
		msg := prefsMsg(t, p)
		honest := decodePartial(t, msg).Messages[0]

		out := Plan(faults.Prefs34Apart, msg, p, now, 0)

		require.Len(t, out, 7)
		require.Same(t, msg, out[0].Msg)
		require.False(t, out[0].Resign)
		for _, o := range out {
			require.Equal(t, p, o.Slot, "the topic stays P for every row")
		}

		// Rows 0-3 and 5 are at payload slot P and carry five distinct roots.
		roots := map[[32]byte]bool{}
		for _, i := range []int{0, 1, 2, 3, 5} {
			b := decodePartial(t, out[i].Msg)
			require.Equal(t, p, b.Slot, "row %d", i)
			roots[b.Messages[0].SigningRoot] = true
		}
		require.Len(t, roots, 5)

		for i := 1; i <= 3; i++ {
			require.True(t, out[i].Resign)
			require.Equal(t, time.Duration(i)*100*time.Millisecond, out[i].Delay)
			require.Zero(t, out[i].DelaySlots)
			require.Equal(t, "p", out[i].Label)
		}

		q1 := decodePartial(t, out[4].Msg)
		require.Equal(t, p+34, q1.Slot)
		require.Equal(t, 18, out[4].DelaySlots, "slot 1 of epoch(P)")
		require.Equal(t, "p+34", out[4].Label)
		require.False(t, roots[q1.Messages[0].SigningRoot], "q1 is a root of its own")

		require.Equal(t, 19, out[5].DelaySlots, "slot 2 of epoch(P)")
		require.Equal(t, "p-5th", out[5].Label)

		rep := decodePartial(t, out[6].Msg)
		require.Equal(t, p+34, rep.Slot)
		require.Equal(t, q1.Messages[0].SigningRoot, rep.Messages[0].SigningRoot, "the repeat keeps q1's root")
		require.NotEqual(t, q1.Messages[0].PartialSignature, rep.Messages[0].PartialSignature)
		require.Equal(t, 20, out[6].DelaySlots)
		require.Equal(t, "p+34-repeat", out[6].Label)

		for _, o := range out[1:] {
			require.True(t, o.Resign)
			require.Equal(t, honest.Signer, decodePartial(t, o.Msg).Messages[0].Signer)
		}
	})

	identityCases := []struct {
		name string
		msg  func(t *testing.T) *spectypes.SignedSSVMessage
		slot phase0.Slot
		now  phase0.Slot
	}{
		{"same-epoch emission (restart tick)", func(t *testing.T) *spectypes.SignedSSVMessage { return prefsMsg(t, 340) }, 340, now},
		{"k = 30: P+34 would leave epoch(P)+1", func(t *testing.T) *spectypes.SignedSSVMessage { return prefsMsg(t, 382) }, 382, now},
		{"k = 31", func(t *testing.T) *spectypes.SignedSSVMessage { return prefsMsg(t, 383) }, 383, now},
		{"two epochs ahead", func(t *testing.T) *spectypes.SignedSSVMessage { return prefsMsg(t, 390) }, 390, now},
		{"a type-9 packet", func(t *testing.T) *spectypes.SignedSSVMessage { return authMsg(t, p, 1) }, p, now},
		{"a non-role-8 message", func(t *testing.T) *spectypes.SignedSSVMessage {
			return partialMsg(t, spectypes.RolePTCAttester, p)
		}, p, now},
	}
	for _, tc := range identityCases {
		t.Run("identity: "+tc.name, func(t *testing.T) {
			msg := tc.msg(t)
			out := Plan(faults.Prefs34Apart, msg, tc.slot, tc.now, 0)
			require.Len(t, out, 1)
			require.Same(t, msg, out[0].Msg)
		})
	}
}

// prefs-early-epoch (MSG-04), addendum D.4: the bound for T = FirstSlotAtEpoch(epoch(now)+2) is
// the start of epoch(now)+1.
func TestPlanPrefsEarlyEpoch(t *testing.T) {
	t.Run("steady-state emission at j = 15", func(t *testing.T) {
		msg := prefsMsg(t, 357)
		honest := decodePartial(t, msg).Messages[0]

		out := Plan(faults.PrefsEarlyEpoch, msg, 357, 335, 0)

		require.Len(t, out, 4, "honest, probe, control, control proof")
		require.Same(t, msg, out[0].Msg)

		probe, control, proof := out[1], out[2], out[3]
		for _, o := range out[1:] {
			require.True(t, o.Resign)
			require.Equal(t, phase0.Slot(357), o.Slot, "the topic follows the original slot")
			require.Equal(t, phase0.Slot(384), decodePartial(t, o.Msg).Slot, "T = first slot of epoch 12")
		}
		require.Equal(t, 16, probe.DelaySlots, "arrives in slot 31 of epoch 10, one slot before the bound")
		require.Zero(t, probe.Delay)
		require.Equal(t, "probe", probe.Label)
		require.Equal(t, 17, control.DelaySlots, "arrives at slot 0 of epoch 11, exactly the bound")
		require.Zero(t, control.Delay)
		require.Equal(t, "control", control.Label)
		require.Equal(t, 17, proof.DelaySlots)
		require.Equal(t, 2*time.Second, proof.Delay)
		require.Equal(t, "control-proof", proof.Label)

		pr := decodePartial(t, probe.Msg).Messages[0]
		cr := decodePartial(t, control.Msg).Messages[0]
		rr := decodePartial(t, proof.Msg).Messages[0]
		require.NotEqual(t, honest.SigningRoot, pr.SigningRoot)
		require.NotEqual(t, honest.SigningRoot, cr.SigningRoot)
		require.NotEqual(t, pr.SigningRoot, cr.SigningRoot, "probe and control are distinct roots")
		require.Equal(t, cr.SigningRoot, rr.SigningRoot, "the proof repeats the control root")
		require.NotEqual(t, cr.PartialSignature, rr.PartialSignature, "and differs in bytes")
	})

	t.Run("emission at j = 31 sends the probe now", func(t *testing.T) {
		msg := prefsMsg(t, 360)
		out := Plan(faults.PrefsEarlyEpoch, msg, 360, 351, 0)
		require.Len(t, out, 4)
		require.Equal(t, phase0.Slot(384), decodePartial(t, out[1].Msg).Slot)
		require.Zero(t, out[1].DelaySlots)
		require.Equal(t, 1, out[2].DelaySlots)
	})

	t.Run("identity for a non-preferences message", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RolePTCAttester, 357)
		out := Plan(faults.PrefsEarlyEpoch, msg, 357, 335, 0)
		require.Len(t, out, 1)
		require.Same(t, msg, out[0].Msg)
	})

	t.Run("identity for a type-9 packet", func(t *testing.T) {
		msg := authMsg(t, 357, 1)
		out := Plan(faults.PrefsEarlyEpoch, msg, 357, 335, 0)
		require.Len(t, out, 1)
	})
}

func TestPlanPrefsEarlyAndLate(t *testing.T) {
	t.Run("early is 65 slots ahead of now, payload slot shifted forward", func(t *testing.T) {
		msg := prefsMsg(t, 200)
		out := Plan(faults.PrefsEarly, msg, 200, 150, 0)
		require.Len(t, out, 2)
		require.Same(t, msg, out[0].Msg)
		require.True(t, out[1].Resign)
		require.Equal(t, phase0.Slot(215), decodePartial(t, out[1].Msg).Slot)
		require.Equal(t, phase0.Slot(200), out[1].Slot, "the topic must follow the original slot")
	})

	// FIX 2: prefs-late used to shift the PAYLOAD slot backward (now - 3), which lands on
	// ErrNoDuty at validateBeaconDuty (message/validation/common_checks.go's RoleProposerPreferences
	// branch, called before validateSlotTime) rather than on the lateness rule — see prefsLate's doc
	// comment in plan.go for the full trap. The fixed version keeps the payload slot on the honest
	// proposal slot and instead delays the SEND, via DelaySlots, and carries a distinct signing root
	// so a duplicate-root rejection (validateDistinctRootBudget, which also runs before
	// validateSlotTime) doesn't pre-empt the lateness rule either.
	t.Run("late arrives via a delayed send; the payload slot and topic are untouched", func(t *testing.T) {
		msg := prefsMsg(t, 200)
		honestRoot := decodePartial(t, msg).Messages[0].SigningRoot

		out := Plan(faults.PrefsLate, msg, 200, 150, 0)

		require.Len(t, out, 2)
		require.Same(t, msg, out[0].Msg)

		late := out[1]
		require.True(t, late.Resign)
		require.Equal(t, phase0.Slot(200), late.Slot, "the topic must follow the original slot")
		require.Zero(t, late.Delay, "Plan never converts a slot offset into a duration; the decorator does")
		require.Equal(t, 53, late.DelaySlots, "body.Slot(200) + prefsLateSlots(3) - now(150)")

		body := decodePartial(t, late.Msg)
		require.Equal(t, phase0.Slot(200), body.Slot,
			"the payload slot must stay the honest proposal slot so the assignment gate passes")
		require.NotEqual(t, honestRoot, body.Messages[0].SigningRoot,
			"a same-root copy is refused as a duplicate before the lateness rule is ever reached")
	})

	t.Run("no delayed copy once the target slot has already passed", func(t *testing.T) {
		msg := prefsMsg(t, 200)
		// body.Slot(200) + prefsLateSlots(3) - now(203) == 0: not positive, so DelaySlots would be
		// zero or negative and Plan sends only the honest message.
		out := Plan(faults.PrefsLate, msg, 200, 203, 0)
		require.Len(t, out, 1, "no room to be late anymore, send only the honest message")
	})
}

func TestPlanPrefsReplay(t *testing.T) {
	msg := prefsMsg(t, 200)
	honestRoot := decodePartial(t, msg).Messages[0].SigningRoot

	out := Plan(faults.PrefsReplay, msg, 200, 150, 0)

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

	out := Plan(faults.PTCQBFT, msg, 200, 200, 0)

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

	out := Plan(faults.TwoEntries, msg, 200, 200, 0)

	// The honest message goes out too: the entry-count rule is structural (semantics) and runs
	// before any signer state or message-limit check, so the honest partial cannot pre-empt it.
	// See dupEntry's doc comment.
	require.Len(t, out, 2)
	require.Same(t, msg, out[0].Msg, "the honest message is sent unchanged")
	require.False(t, out[0].Resign)
	require.NotSame(t, msg, out[1].Msg, "must be a clone, not the original")
	require.Len(t, decodePartial(t, out[1].Msg).Messages, 2)
	require.True(t, out[1].Resign)
}

// vr-prefork-slot-replay (MSG-02 gate 3): the fork is at epoch 3, so preForkSlot = 95.
func TestPlanVRPreForkSlotReplay(t *testing.T) {
	const preForkSlot = phase0.Slot(95)

	t.Run("a role-7 partial in epoch fork+1 is cloned as a pre-fork role-4 partial", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RolePTCAttester, 130)
		honest := decodePartial(t, msg).Messages[0]

		out := Plan(faults.VRPreForkSlotReplay, msg, 130, 130, preForkSlot)

		require.Len(t, out, 2)
		require.Same(t, msg, out[0].Msg)
		require.False(t, out[0].Resign)

		c := out[1]
		require.True(t, c.Resign)
		require.Equal(t, phase0.Slot(130), c.Slot, "the topic follows the live slot")
		id := c.Msg.SSVMessage.GetID()
		require.Equal(t, spectypes.RoleValidatorRegistration, id.GetRoleType())
		require.Equal(t, msg.SSVMessage.GetID().GetDutyExecutorID(), id.GetDutyExecutorID(), "same validator")
		require.Equal(t, msg.SSVMessage.GetID().GetDomain(), id.GetDomain())
		body := decodePartial(t, c.Msg)
		require.Equal(t, spectypes.ValidatorRegistrationPartialSig, body.Type)
		require.Equal(t, preForkSlot, body.Slot)
		require.Len(t, body.Messages, 1)
		require.Equal(t, honest.ValidatorIndex, body.Messages[0].ValidatorIndex)
		require.Equal(t, honest.Signer, body.Messages[0].Signer)
		require.Equal(t, honest.SigningRoot, body.Messages[0].SigningRoot)
	})

	t.Run("identity in the fork epoch itself", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RolePTCAttester, 100)
		out := Plan(faults.VRPreForkSlotReplay, msg, 100, 100, preForkSlot)
		require.Len(t, out, 1)
		require.Same(t, msg, out[0].Msg)
	})

	t.Run("identity for a non-role-7 input", func(t *testing.T) {
		for _, r := range []spectypes.RunnerRole{spectypes.RoleProposerPreferences, spectypes.RoleProposer, spectypes.RoleCommittee} {
			msg := partialMsg(t, r, 130)
			out := Plan(faults.VRPreForkSlotReplay, msg, 130, 130, preForkSlot)
			require.Len(t, out, 1, "role %v", r)
		}
	})

	t.Run("identity for a role-7 consensus message", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RolePTCAttester, 130)
		msg.SSVMessage.MsgType = spectypes.SSVConsensusMsgType
		out := Plan(faults.VRPreForkSlotReplay, msg, 130, 130, preForkSlot)
		require.Len(t, out, 1)
	})

	t.Run("identity when preForkSlot is not below now (far-future wrap)", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RolePTCAttester, 130)
		out := Plan(faults.VRPreForkSlotReplay, msg, 130, 130, 500)
		require.Len(t, out, 1)
	})
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

		out := Plan(faults.PTC3PerEpoch, msg, 200, 200, 0)

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
		out := Plan(faults.PTC3PerEpoch, msg, 223, 223, 0)
		require.Len(t, out, 1)
	})

	t.Run("room for only one extra slot", func(t *testing.T) {
		// Slot 222 has room for 223 (same epoch) but not 224 (next epoch): all-or-nothing means
		// neither extra copy is sent.
		msg := partialMsg(t, spectypes.RolePTCAttester, 222)
		out := Plan(faults.PTC3PerEpoch, msg, 222, 222, 0)
		require.Len(t, out, 1)
	})
}

func TestPlanRole7PreFork(t *testing.T) {
	const preForkSlot = phase0.Slot(64)

	assertForged := func(t *testing.T, out []Outgoing, msg *spectypes.SignedSSVMessage) {
		t.Helper()
		require.Len(t, out, 3, "the honest message plus roles 7 and 8")
		require.Same(t, msg, out[0].Msg)
		require.False(t, out[0].Resign)

		roles := []spectypes.RunnerRole{
			out[1].Msg.SSVMessage.GetID().GetRoleType(),
			out[2].Msg.SSVMessage.GetID().GetRoleType(),
		}
		require.ElementsMatch(t, []spectypes.RunnerRole{
			spectypes.RolePTCAttester, spectypes.RoleProposerPreferences,
		}, roles)
		for _, o := range out[1:] {
			require.True(t, o.Resign)
			require.Equal(t, preForkSlot, decodePartial(t, o.Msg).Slot,
				"the forged messages carry the decorator-supplied pre-fork slot, not the honest message's own slot")
		}
	}

	t.Run("still fires on a validator-registration partial", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RoleValidatorRegistration, 500)
		out := Plan(faults.Role7PreFork, msg, 500, 500, preForkSlot)
		assertForged(t, out, msg)
	})

	// FIX 4: role7-prefork used to trigger only on an outgoing RoleValidatorRegistration partial.
	// Post-fork there are none: vr-postfork (MSG-02's other half) is what keeps that heartbeat
	// running, and the pass procedure waits for the fork before iterating the menu — so a VR-only
	// trigger would never fire in practice, leaving MSG-02 with zero coverage. It must fire on ANY
	// outgoing partial-signature message.
	t.Run("fires on any outgoing partial-signature message, e.g. a PTC partial", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RolePTCAttester, 500)
		out := Plan(faults.Role7PreFork, msg, 500, 500, preForkSlot)
		assertForged(t, out, msg)
	})

	t.Run("leaves a non-partial-signature message alone", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RoleValidatorRegistration, 500)
		msg.SSVMessage.MsgType = spectypes.SSVConsensusMsgType // not a partial-sig message
		out := Plan(faults.Role7PreFork, msg, 500, 500, preForkSlot)
		require.Len(t, out, 1)
		require.Same(t, msg, out[0].Msg)
	})

	// FIX B (second review cycle): a committee-role partial's MsgID executor is 16 zero bytes plus
	// the 32-byte committee ID, not a validator pubkey (see NewCommitteeMsgID in ssv-spec). Forging
	// it into a validator-role clone sends the publish path down the validator branch
	// (network/p2p/p2p_pubsub.go's BroadcastAtSlot), where ValidatorStore().Validator(executorID)
	// can never resolve those bytes — nothing would reach the wire, only three failed-send warnings
	// per committee partial alongside three "injected" lines that outrun them. Both committee-shaped
	// roles must return identity, untouched, one Outgoing, same pointer, unsigned.
	t.Run("skips a committee-role partial: its executor is not a validator pubkey", func(t *testing.T) {
		for _, r := range []spectypes.RunnerRole{spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee} {
			msg := partialMsg(t, r, 500)
			out := Plan(faults.Role7PreFork, msg, 500, 500, preForkSlot)
			require.Len(t, out, 1, "role %v must yield identity only", r)
			require.Same(t, msg, out[0].Msg, "role %v must return the same pointer", r)
			require.False(t, out[0].Resign, "role %v must not be resigned", r)
		}
	})

	// "Also (cheap)": GloasForkEpoch() reports ok for an unscheduled far-future epoch by design
	// (networkconfig/beacon.go), pinned to FarFutureEpoch = math.MaxUint64
	// (beacon/goclient/types.go). Multiplying that by SlotsPerEpoch wraps a uint64 into a slot with
	// no relation to any real pre-fork boundary — a slot that is not, in fact, below the current
	// wall clock. Standing in for that wrapped value directly (rather than reproducing the
	// multiplication here) keeps this test about Plan's guard, not about the wraparound arithmetic
	// itself, which TestRole7PreForkSlot below does not (and should not) reproduce either.
	t.Run("treats a wrapped pre-fork slot as inapplicable and returns identity", func(t *testing.T) {
		msg := partialMsg(t, spectypes.RolePTCAttester, 500)
		wrappedPreForkSlot := phase0.Slot(500) // not below now(500): exactly the wraparound symptom
		out := Plan(faults.Role7PreFork, msg, 500, 500, wrappedPreForkSlot)
		require.Len(t, out, 1, "a pre-fork slot that is not below now must not be forged against")
		require.Same(t, msg, out[0].Msg)
		require.False(t, out[0].Resign)
	})
}

// TestPerturbForRepeat pins the FIX 3 fix directly: perturbForRepeat is called by the decorator
// (faultnet.go sendAsync), not by Plan, so it is tested here as its own pure function rather than
// through the whole async send series (which faultnet's dispatch_test.go exercises for the
// observable delivery shape — see TestDispatchRepeatedSendAnnouncesOnceAndSummarizes there).
//
// Pipeline reasoning: SignSSVMessage is deterministic (RSA PKCS1v15), so re-signing byte-identical
// Data on every repeat would re-encode to the exact same SignedSSVMessage every time, and
// gossipsub's own dedup (network/topics/msg_id.go) silently drops every send after the first before
// it ever reaches this node's peers — the fault would flood nothing but this node's own "sent"
// counter. Perturbing the first four bytes of PartialSignature (which validation never reads)
// before each repeat keeps every send in the series byte-distinct on the wire, while leaving the
// signing root — the rule prefs-replay is actually testing — untouched.
//
// FIX A (second review cycle): this used to write the counter into a single byte
// (PartialSignature[0] = byte(i)), which aliases every 256 iterations — send #1 and send #257
// encoded identically, and prefs-replay's real series is 15,841 sends long, so that byte wrapped
// around ~62 times. Writing the counter across four little-endian bytes instead gives ~4 billion
// distinct values, so this test asserts distinctness with a uint32 key (not a byte key) and pins
// the specific 1-vs-257 aliasing boundary the old code could not cross.
func TestPerturbForRepeat(t *testing.T) {
	msg := prefsMsg(t, 200)
	honestRoot := decodePartial(t, msg).Messages[0].SigningRoot

	seen := map[uint32]bool{}
	for _, i := range []int{1, 2, 3, 255, 256, 257, 258, 512, 4096} {
		require.NoError(t, perturbForRepeat(msg, i))
		body := decodePartial(t, msg)
		got := binary.LittleEndian.Uint32(body.Messages[0].PartialSignature[:4])
		require.Equal(t, uint32(i), got, "the counter must round-trip through all four bytes")
		require.False(t, seen[got], "each repeat must be distinct from the ones before it")
		seen[got] = true
		require.Equal(t, honestRoot, body.Messages[0].SigningRoot, "the signing root must never move")
	}

	// Direct assertion that iterations 1 and 257 no longer encode identically: under the old
	// single-byte write, byte(1) == byte(257) == 0x01, so send #257 would have been byte-identical
	// to send #1 and gossipsub would have dropped it as a duplicate of a message already inside its
	// own seen-cache TTL.
	require.NoError(t, perturbForRepeat(msg, 1))
	oneBytes := append([]byte(nil), decodePartial(t, msg).Messages[0].PartialSignature[:4]...)
	require.NoError(t, perturbForRepeat(msg, 257))
	twoFiftySevenBytes := decodePartial(t, msg).Messages[0].PartialSignature[:4]
	require.NotEqual(t, oneBytes, twoFiftySevenBytes, "iteration 257 must no longer alias iteration 1")
}

// TestPerturbForRepeatNoopOnEmptyMessages pins the defensive guard: perturbForRepeat degrades to a
// no-op (nil error, no mutation) rather than panicking on an out-of-range index when there is no
// PartialSignatureMessage to perturb. PartialSignature itself is SSZ-fixed at 96 bytes
// (ssv-spec's PartialSignatureMessage), so a too-short signature can never round-trip through
// Encode/Decode to exercise the four-byte-length half of the same guard directly — this is the
// realistically-reachable half of it (a real message can carry zero entries; Messages is an
// SSZ list, `ssz-max:"5048"`, with no lower bound).
func TestPerturbForRepeatNoopOnEmptyMessages(t *testing.T) {
	msg := prefsMsg(t, 200)
	body := decodePartial(t, msg)
	body.Messages = nil
	require.NoError(t, setPartialBody(msg, body))
	before := append([]byte(nil), msg.SSVMessage.Data...)

	require.NoError(t, perturbForRepeat(msg, 1))
	require.Equal(t, before, msg.SSVMessage.Data, "an empty Messages list must be left untouched")
}

// TestRole7PreForkSlot is Network.role7PreForkSlot, defined in faultnet.go beside BroadcastAtSlot —
// it lives here because plan_test.go already has the phase0/networkconfig test scaffolding this
// needs, and faultnet.go itself has none of its own test file. Pins FIX 4's config-to-Plan seam: the
// decorator, not Plan, computes the pre-fork slot from GLOAS_FORK_EPOCH * SlotsPerEpoch.
func TestRole7PreForkSlot(t *testing.T) {
	t.Run("one slot below the fork boundary", func(t *testing.T) {
		gloasEpoch := phase0.Epoch(10)
		cfg := networkconfig.TestNetworkWithGloas(gloasEpoch) // clones TestNetwork; safe to mutate

		n := &Network{netCfg: cfg}
		want := phase0.Slot(uint64(gloasEpoch)*cfg.SlotsPerEpoch) - 1
		require.Equal(t, want, n.role7PreForkSlot())
		require.True(t, n.netCfg.IsGloasAtSlot(want+1), "sanity: the boundary slot itself must be Gloas")
		require.False(t, n.netCfg.IsGloasAtSlot(want), "sanity: the returned slot must be pre-fork")
	})

	t.Run("falls back to 0 when there is no scheduled Gloas fork", func(t *testing.T) {
		// TestNetwork itself has no Gloas fork scheduled (see TestNetworkWithGloas's doc comment).
		n := &Network{netCfg: networkconfig.TestNetwork}
		require.Zero(t, n.role7PreForkSlot())
	})
}
