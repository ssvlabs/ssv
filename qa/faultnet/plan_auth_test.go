package faultnet

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/qa/faults"
)

// authMsg builds this node's type-9 request-auth packet for proposal slot slot with n entries,
// the shape runRequestAuthRound broadcasts (one entry per configured builder; the QA synthetic
// builder gives exactly one).
func authMsg(t *testing.T, slot phase0.Slot, n int) *spectypes.SignedSSVMessage {
	t.Helper()
	msg := partialMsg(t, spectypes.RoleProposerPreferences, slot)
	body := decodePartial(t, msg)
	body.Type = spectypes.RequestAuthPartialSig
	tmpl := body.Messages[0]
	body.Messages = nil
	for i := 0; i < n; i++ {
		e := *tmpl
		e.PartialSignature = append([]byte(nil), tmpl.PartialSignature...)
		e.SigningRoot[5] = byte(0xa0 + i)
		body.Messages = append(body.Messages, &e)
	}
	require.NoError(t, setPartialBody(msg, body))
	return msg
}

func entryRoots(t *testing.T, o Outgoing) [][32]byte {
	t.Helper()
	var out [][32]byte
	for _, m := range decodePartial(t, o.Msg).Messages {
		out = append(out, m.SigningRoot)
	}
	return out
}

func distinct(roots [][32]byte) map[[32]byte]bool {
	m := map[[32]byte]bool{}
	for _, r := range roots {
		m[r] = true
	}
	return m
}

func requireUniformEntries(t *testing.T, o Outgoing, signer spectypes.OperatorID, index phase0.ValidatorIndex) {
	t.Helper()
	for _, m := range decodePartial(t, o.Msg).Messages {
		require.Equal(t, signer, m.Signer)
		require.Equal(t, index, m.ValidatorIndex)
	}
}

// requireAuthIdentity checks the identity cases every auth value shares.
func requireAuthIdentity(t *testing.T, f faults.Fault) {
	t.Helper()
	for name, msg := range map[string]*spectypes.SignedSSVMessage{
		"type-8 packet":      prefsMsg(t, 200),
		"non-role-8 message": partialMsg(t, spectypes.RolePTCAttester, 200),
		"two-entry type-9":   authMsg(t, 200, 2),
		"consensus under r 8": func() *spectypes.SignedSSVMessage {
			m := authMsg(t, 200, 1)
			m.SSVMessage.MsgType = spectypes.SSVConsensusMsgType
			return m
		}(),
	} {
		out := Plan(f, msg, 200, 185, 0)
		require.Len(t, out, 1, name)
		require.Same(t, msg, out[0].Msg, name)
	}
}

func TestPlanAuth9Entries(t *testing.T) {
	msg := authMsg(t, 200, 1)
	honest := decodePartial(t, msg).Messages[0]
	r0 := honest.SigningRoot

	out := Plan(faults.Auth9Entries, msg, 200, 185, 0)

	require.Len(t, out, 4)
	require.Same(t, msg, out[0].Msg)
	require.False(t, out[0].Resign)

	nine, eight, ninth := out[1], out[2], out[3]

	require.Len(t, decodePartial(t, nine.Msg).Messages, 9)
	require.Len(t, distinct(entryRoots(t, nine)), 9)
	require.False(t, distinct(entryRoots(t, nine))[r0])
	requireUniformEntries(t, nine, honest.Signer, honest.ValidatorIndex)
	require.Equal(t, "9-entries", nine.Label)

	eightRoots := entryRoots(t, eight)
	require.Len(t, eightRoots, 8)
	require.Equal(t, r0, eightRoots[0], "the 8-entry packet carries the recorded honest root first")
	require.Len(t, distinct(eightRoots), 8)
	requireUniformEntries(t, eight, honest.Signer, honest.ValidatorIndex)
	require.Equal(t, "8-root", eight.Label)

	ninthRoots := entryRoots(t, ninth)
	require.Len(t, ninthRoots, 1)
	require.False(t, distinct(eightRoots)[ninthRoots[0]], "the 9th root is new")
	require.Equal(t, "9th-root", ninth.Label)

	require.Zero(t, nine.Delay)
	require.Equal(t, time.Second, eight.Delay)
	require.Equal(t, 2*time.Second, ninth.Delay)
	for _, o := range out[1:] {
		require.True(t, o.Resign)
		require.Equal(t, phase0.Slot(200), o.Slot)
		b := decodePartial(t, o.Msg)
		require.Equal(t, spectypes.RequestAuthPartialSig, b.Type)
		require.Equal(t, phase0.Slot(200), b.Slot)
	}

	requireAuthIdentity(t, faults.Auth9Entries)
}

func TestPlanAuthMixedSigner(t *testing.T) {
	msg := authMsg(t, 200, 1)
	honest := decodePartial(t, msg).Messages[0]
	r0 := honest.SigningRoot

	out := Plan(faults.AuthMixedSigner, msg, 200, 185, 0)

	require.Len(t, out, 3)
	require.Same(t, msg, out[0].Msg)

	mixedIndex := decodePartial(t, out[1].Msg).Messages
	require.Len(t, mixedIndex, 2)
	require.Equal(t, mixedIndex[0].Signer, mixedIndex[1].Signer)
	require.Equal(t, honest.ValidatorIndex, mixedIndex[0].ValidatorIndex)
	require.Equal(t, honest.ValidatorIndex+1, mixedIndex[1].ValidatorIndex)
	require.Equal(t, "mixed-index", out[1].Label)

	mixedSigner := decodePartial(t, out[2].Msg).Messages
	require.Len(t, mixedSigner, 2)
	require.Equal(t, mixedSigner[0].ValidatorIndex, mixedSigner[1].ValidatorIndex)
	require.Equal(t, honest.Signer, mixedSigner[0].Signer, "the first entry matches the outer signer")
	require.Equal(t, honest.Signer+1, mixedSigner[1].Signer)
	require.Equal(t, "mixed-signer", out[2].Label)

	roots := append(entryRoots(t, out[1]), entryRoots(t, out[2])...)
	require.Len(t, distinct(roots), 4)
	require.False(t, distinct(roots)[r0])

	for _, o := range out[1:] {
		require.True(t, o.Resign)
		require.Equal(t, msg.OperatorIDs, o.Msg.OperatorIDs, "outer OperatorIDs are set by send, not by Plan")
	}

	requireAuthIdentity(t, faults.AuthMixedSigner)
}

func TestPlanAuthRepeatRootInPacket(t *testing.T) {
	msg := authMsg(t, 200, 1)
	honest := decodePartial(t, msg).Messages[0]
	r0 := honest.SigningRoot

	out := Plan(faults.AuthRepeatRootInPacket, msg, 200, 185, 0)

	require.Len(t, out, 5)
	require.Same(t, msg, out[0].Msg)

	five := entryRoots(t, out[1])
	require.Len(t, five, 5)
	require.Len(t, distinct(five), 5)
	require.False(t, distinct(five)[r0])

	rep := entryRoots(t, out[2])
	require.Len(t, rep, 3)
	require.Equal(t, rep[0], rep[1], "one root repeated in two entries")
	require.NotEqual(t, rep[0], rep[2])

	sub := entryRoots(t, out[3])
	require.Len(t, sub, 2)
	for _, r := range sub {
		require.True(t, distinct(rep)[r], "row 3 roots are a subset of row 2")
	}
	require.NotEqual(t, out[2].Msg.SSVMessage.Data, out[3].Msg.SSVMessage.Data)

	over := entryRoots(t, out[4])
	require.Len(t, over, 3)
	earlier := distinct(append(append(append([][32]byte{r0}, five...), rep...), sub...))
	for _, r := range over {
		require.False(t, earlier[r], "row 4 roots are disjoint from rows 0-3")
	}
	require.Len(t, distinct(over), 3)
	require.Len(t, earlier, 8, "rows 0-2 record exactly 8 distinct roots")

	for i := 1; i < len(out); i++ {
		require.True(t, out[i].Resign)
		require.Greater(t, out[i].Delay, out[i-1].Delay, "delays strictly increase")
		requireUniformEntries(t, out[i], honest.Signer, honest.ValidatorIndex)
	}
	require.Equal(t, []string{"5-roots", "repeat-in-packet", "no-new-root", "over-budget"},
		[]string{out[1].Label, out[2].Label, out[3].Label, out[4].Label})

	requireAuthIdentity(t, faults.AuthRepeatRootInPacket)
}
