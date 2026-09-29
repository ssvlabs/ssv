package faultnet

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// The request-auth (type 9) wire values of §5 (MSG-03, MSG-10), addendum D.8-D.11.
//
// Precondition: the faulted operator must emit type-9 packets at all, so cli/operator/node.go's
// applyBuilderFault gives it one synthetic builder for these values as for auth-no-builders. The
// honest packet is then {r0}, one entry, and it is the first message for (P, signer): it creates
// the signer state without a budget check (runRequestAuthRound runs before the type-8 broadcast,
// runner/proposer_preferences.go:523). Each value keeps that honest identity send first and adds
// re-signed clones. Every clone keeps the honest Signer, ValidatorIndex, payload slot and topic
// unless its row says otherwise; its roots come from fabricateRoot (byte 31), disjoint from the
// byte-0 flips of the preference values. The type-9 and type-8 root sets of one (P, signer) are
// separate (message/validation/signer_state.go:36-37), so none of these values moves the §5
// preference budget.
//
// Validation order (partial_validation.go): the entry cap (:139-146, maxRequestAuthEntries = 8),
// the signer rule (:153-158) and the same-index rule (:164-169) are structural and REJECT before
// any budget; the distinct-root budget (:391-415, 8 roots, IGNORE) runs only once a signer state
// exists. Receivers with no Builders then fail the accepted packets in the runner with
// "no builders configured" (runner/proposer_preferences_request_auth.go:139), on either the queue
// path or the stash-replay path.
//
// Plan fires only on a single-entry honest packet: that is the synthetic-builder shape, and the
// oracle counts below assume it. A real multi-builder configuration yields identity.

// isRequestAuth reports whether msg is this node's own single-entry request-auth packet, and
// returns its only entry.
func isRequestAuth(msg *spectypes.SignedSSVMessage) *spectypes.PartialSignatureMessage {
	if role(msg) != spectypes.RoleProposerPreferences {
		return nil
	}
	body := partialBody(msg)
	if body == nil || body.Type != spectypes.RequestAuthPartialSig || len(body.Messages) != 1 {
		return nil
	}
	return body.Messages[0]
}

// authEntry returns a copy of tmpl carrying root, with its own partial-signature slice.
func authEntry(tmpl *spectypes.PartialSignatureMessage, root [32]byte) *spectypes.PartialSignatureMessage {
	e := *tmpl
	e.PartialSignature = append(spectypes.Signature(nil), tmpl.PartialSignature...)
	e.SigningRoot = root
	return &e
}

// authPacket clones msg with its entries replaced by entries built from tmpl, one per root, each
// optionally adjusted by edit (nil for none).
func authPacket(msg *spectypes.SignedSSVMessage, tmpl *spectypes.PartialSignatureMessage, roots [][32]byte,
	edit func(i int, e *spectypes.PartialSignatureMessage)) (*spectypes.SignedSSVMessage, bool) {
	return clonePartial(msg, func(b *spectypes.PartialSignatureMessages) {
		b.Messages = make([]*spectypes.PartialSignatureMessage, 0, len(roots))
		for i, r := range roots {
			e := authEntry(tmpl, r)
			if edit != nil {
				edit(i, e)
			}
			b.Messages = append(b.Messages, e)
		}
	})
}

// authRoots returns fabricateRoot(r0, i) for each i.
func authRoots(r0 [32]byte, is ...byte) [][32]byte {
	out := make([][32]byte, 0, len(is))
	for _, i := range is {
		out = append(out, fabricateRoot(r0, i))
	}
	return out
}

// authNineEntries serves auth-9-entries (MSG-03, MSG-10), addendum D.8:
//
//	#  packet                        send    honest verdict
//	0  {r0} (honest)                 now     accept (1 root recorded)
//	1  9 entries r0^1..r0^9          now     REJECT "too many signatures in a partial-signature message, got 9, want 8" (nothing recorded)
//	2  8 entries {r0, a1..a7}        +1 s    accept (1 seen + 7 new = 8)
//	3  {a8}                          +2 s    IGNORE "request-auth, 8 distinct root(s) seen and 1 new"
//
// Row 2 includes r0: 8 fresh roots on top of the recorded r0 would be IGNOREd "1 distinct root(s)
// seen and 8 new" instead (the MSG-10 card's sequence is wrong there). Row 1 costs a gossipsub p4
// penalty, so the decorator caps this value (budget.go). The receiver-runner cap
// "request-auth packet carries %d entries" is unreachable from the wire: row 1 is REJECTed first.
func authNineEntries(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	tmpl := isRequestAuth(msg)
	if tmpl == nil {
		return identity
	}
	r0 := tmpl.SigningRoot

	nine, ok := authPacket(msg, tmpl, authRoots(r0, 1, 2, 3, 4, 5, 6, 7, 8, 9), nil)
	if !ok {
		return identity
	}
	eight, ok := authPacket(msg, tmpl, append([][32]byte{r0}, authRoots(r0, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17)...), nil)
	if !ok {
		return identity
	}
	ninth, ok := authPacket(msg, tmpl, authRoots(r0, 0x18), nil)
	if !ok {
		return identity
	}
	return append(identity,
		Outgoing{Msg: nine, Slot: slot, Resign: true, Label: "9-entries"},
		Outgoing{Msg: eight, Slot: slot, Resign: true, Delay: time.Second, Label: "8-root"},
		Outgoing{Msg: ninth, Slot: slot, Resign: true, Delay: 2 * time.Second, Label: "9th-root"},
	)
}

// authMixedSigner serves auth-mixed-signer (MSG-03, MSG-10), addendum D.9. §6.3 defines it as
// "entries with two validator indices"; a differing Signer is a second rule, so both are sent:
//
//	#  packet                                honest verdict
//	0  {r0} (honest)                         accept
//	1  {r0^1 (index vi), r0^2 (index vi+1)}  REJECT "validator index differs across partial signatures, got <vi+1>, want <vi>"
//	2  {r0^3 (signer s), r0^4 (signer s+1)}  REJECT "signer is not expected, got <s>, want <s+1>"
//
// The same-index rule runs before membership (partial_validation.go:164 vs :175), so vi+1 need not
// exist. send re-signs with this operator, so the outer signer is s and the first entry of row 2
// matches it. Both rows cost p4, so the decorator caps this value (budget.go). The runner-level
// validateValidatorIndexInPartialSigMsg is unreachable.
func authMixedSigner(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	tmpl := isRequestAuth(msg)
	if tmpl == nil {
		return identity
	}
	r0 := tmpl.SigningRoot

	mixedIndex, ok := authPacket(msg, tmpl, authRoots(r0, 1, 2), func(i int, e *spectypes.PartialSignatureMessage) {
		if i == 1 {
			e.ValidatorIndex++
		}
	})
	if !ok {
		return identity
	}
	mixedSigner, ok := authPacket(msg, tmpl, authRoots(r0, 3, 4), func(i int, e *spectypes.PartialSignatureMessage) {
		if i == 1 {
			e.Signer++
		}
	})
	if !ok {
		return identity
	}
	return append(identity,
		Outgoing{Msg: mixedIndex, Slot: slot, Resign: true, Delay: 100 * time.Millisecond, Label: "mixed-index"},
		Outgoing{Msg: mixedSigner, Slot: slot, Resign: true, Delay: 200 * time.Millisecond, Label: "mixed-signer"},
	)
}

// authRepeatRootInPacket serves auth-repeat-root-in-packet (MSG-10), addendum D.10:
//
//	#  packet            send    honest verdict
//	0  {r0} (honest)     now     accept (1 recorded)
//	1  {a1..a5}          +1 s    accept (6)
//	2  {a6, a6, a7}      +2 s    accept — the repeat counts once (fresh is a map); 6 + 2 = 8 <= 8
//	3  {a6, a7}          +3 s    IGNORE "request-auth, no new signing root"
//	4  {a8, a9, a10}     +4 s    IGNORE "request-auth, 8 distinct root(s) seen and 3 new"
//
// Discriminator: a double-counting regression turns row 2 into "request-auth, 6 distinct root(s)
// seen and 3 new" (oracle: 0). Each repeated entry still counts toward the 8-entry cap. No REJECT,
// so no peer-score cost and no cap.
func authRepeatRootInPacket(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	tmpl := isRequestAuth(msg)
	if tmpl == nil {
		return identity
	}
	a := func(i byte) [32]byte { return fabricateRoot(tmpl.SigningRoot, 0x20+i) }

	rows := []struct {
		roots [][32]byte
		label string
	}{
		{[][32]byte{a(1), a(2), a(3), a(4), a(5)}, "5-roots"},
		{[][32]byte{a(6), a(6), a(7)}, "repeat-in-packet"},
		{[][32]byte{a(6), a(7)}, "no-new-root"},
		{[][32]byte{a(8), a(9), a(10)}, "over-budget"},
	}
	out := identity
	for i, r := range rows {
		c, ok := authPacket(msg, tmpl, r.roots, nil)
		if !ok {
			return identity
		}
		out = append(out, Outgoing{Msg: c, Slot: slot, Resign: true, Delay: time.Duration(i+1) * time.Second, Label: r.label})
	}
	return out
}
