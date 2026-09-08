package faultnet

import (
	"testing"

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
		require.Len(t, out, 4, "the honest message plus roles 7, 8 and 9")
		require.Same(t, msg, out[0].Msg)
		require.False(t, out[0].Resign)

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
// counter. Perturbing PartialSignature[0] (which validation never reads) before each repeat keeps
// every send in the series byte-distinct on the wire, while leaving the signing root — the rule
// prefs-replay is actually testing — untouched.
func TestPerturbForRepeat(t *testing.T) {
	msg := prefsMsg(t, 200)
	honestRoot := decodePartial(t, msg).Messages[0].SigningRoot

	seen := map[byte]bool{}
	for i := 1; i <= 3; i++ {
		require.NoError(t, perturbForRepeat(msg, i))
		body := decodePartial(t, msg)
		require.Equal(t, byte(i), body.Messages[0].PartialSignature[0])
		require.False(t, seen[body.Messages[0].PartialSignature[0]], "each repeat must be distinct from the ones before it")
		seen[body.Messages[0].PartialSignature[0]] = true
		require.Equal(t, honestRoot, body.Messages[0].SigningRoot, "the signing root must never move")
	}
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
