// Package faultnet decorates the node's P2P network so a selected QA fault can mutate, clone,
// delay or replay the messages this node sends. It is instrumentation for pass M3 of the
// Glamsterdam QA programme and exists only on the qa/gloas-m3-fault-menu branch.
//
// Why the wire is a workable injection point: SSV message validation checks the role-at-slot gate,
// the type-to-role matrix, the entry count, earliness and lateness, the per-epoch duty count and
// the distinct-root budget BEFORE it verifies the operator signature, and it never verifies the
// inner BLS partial signature at all (message/validation/partial_validation.go:20-79). So a
// re-signed, deliberately malformed message reaches exactly the rule under test.
package faultnet

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/qa/faults"
)

// Outgoing is one message the decorator will actually send. Resign is false only on the identity
// path, where the original bytes must survive untouched.
type Outgoing struct {
	Msg    *spectypes.SignedSSVMessage
	Slot   phase0.Slot
	Delay  time.Duration
	Resign bool
	// Repeat is how many extra times to send this message after the first, Every apart. Used by
	// prefs-replay; zero everywhere else.
	Repeat int
	Every  time.Duration
	// DelaySlots is a delay expressed in slots rather than a duration, so a mutator that only knows
	// slot arithmetic (ptc-3-per-epoch) can ask for "send N slots from now" without Plan reading the
	// clock or a network config to convert it. The decorator — which already holds netCfg — turns
	// this into an actual time.Duration added to Delay; see faultnet.go's dispatch.
	DelaySlots int
	// Label names the row of a multi-message fault (prefs-34-apart, the auth-* values, ...). send
	// logs it on the 🧪 line so the tester can confirm every row fired. Empty for single-row faults.
	Label string
}

// Plan returns what to send in place of msg. slot is the slot the caller passed to
// BroadcastAtSlot, which selects the topic; now is the current wall-clock slot; preForkSlot is the
// last slot before the Gloas fork boundary, used by role7-prefork and vr-prefork-slot-replay. Plan
// is pure: it never signs,
// never sends, and never reads the clock or a network config, so every fault is table-testable —
// preForkSlot is computed by the decorator (which already holds the network config) and handed in,
// the same way DelaySlots is converted to a duration by the decorator rather than by Plan.
func Plan(f faults.Fault, msg *spectypes.SignedSSVMessage, slot, now, preForkSlot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}

	if f == faults.None || msg == nil || msg.SSVMessage == nil {
		return identity
	}

	switch f {
	case faults.Prefs34Apart:
		return prefs34Apart(msg, slot, now)
	case faults.Prefs5Roots:
		return prefs5Roots(msg, slot)
	case faults.PrefsEarly:
		return prefsEarly(msg, slot, now)
	case faults.PrefsEarlyEpoch:
		return prefsEarlyEpoch(msg, slot, now)
	case faults.PrefsLate:
		return prefsLate(msg, slot, now)
	case faults.PrefsReplay:
		return prefsReplay(msg, slot)
	case faults.PTCQBFT:
		return forgeConsensusMsg(msg, slot)
	case faults.TwoEntries:
		return dupEntry(msg, slot)
	case faults.PTC3PerEpoch:
		return ptcExtraSlots(msg, slot)
	case faults.Role7PreFork:
		return forgeGloasRoles(msg, slot, now, preForkSlot)
	case faults.VRPreForkSlotReplay:
		return forgePreForkRegistration(msg, slot, now, preForkSlot)
	case faults.Auth9Entries:
		return authNineEntries(msg, slot)
	case faults.AuthMixedSigner:
		return authMixedSigner(msg, slot)
	case faults.AuthRepeatRootInPacket:
		return authRepeatRootInPacket(msg, slot)
	default:
		return identity
	}
}

// Clone deep-copies a message through its own encoding, so a mutation of the copy cannot reach the
// original the node is still using.
func Clone(msg *spectypes.SignedSSVMessage) (*spectypes.SignedSSVMessage, error) {
	b, err := msg.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode message for clone: %w", err)
	}
	c := &spectypes.SignedSSVMessage{}
	if err := c.Decode(b); err != nil {
		return nil, fmt.Errorf("decode cloned message: %w", err)
	}
	return c, nil
}

// role returns the runner role of a message.
func role(msg *spectypes.SignedSSVMessage) spectypes.RunnerRole {
	return msg.SSVMessage.GetID().GetRoleType()
}

// partialBody decodes the partial-signature body, or returns nil when the message is not one.
func partialBody(msg *spectypes.SignedSSVMessage) *spectypes.PartialSignatureMessages {
	if msg.SSVMessage.MsgType != spectypes.SSVPartialSignatureMsgType {
		return nil
	}
	body := &spectypes.PartialSignatureMessages{}
	if err := body.Decode(msg.SSVMessage.Data); err != nil {
		return nil
	}
	return body
}

// setPartialBody re-encodes body into msg.
func setPartialBody(msg *spectypes.SignedSSVMessage, body *spectypes.PartialSignatureMessages) error {
	data, err := body.Encode()
	if err != nil {
		return fmt.Errorf("encode mutated partial signature messages: %w", err)
	}
	msg.SSVMessage.Data = data
	return nil
}

// Replay shape for FLT-11: 66 slots of coverage at 20 messages per second. replayCount is Repeat —
// the number of EXTRA sends after the first — so the series totals replayCount+1 = 15,841 sends.
const (
	replayEvery = 50 * time.Millisecond
	replayCount = 66 * 12 * 1000 / 50 // 66 slots of 12 s, one message every 50 ms, minus the first send
)

// prefsEarlySlots is how far ahead of the emission slot prefs-early stamps its copy. The earliness
// bound is an EPOCH bound, not a slot count: a preference for a slot in epoch E is expected from the
// start of epoch E-(proposerPreferencesEarlyEpochs-1) = E-1 (message/validation/common_checks.go
// messageEarliness :52-65, const.go:47). 65 slots ahead of any emission slot always lands two or
// three epochs ahead, so the copy always crosses that bound; see prefsEarly for the margin.
const prefsEarlySlots = 2*32 + 1

// prefsLateSlots is one slot past the 2-slot preference lateness allowance
// (message/validation/const.go LateSlotAllowance = 2).
const prefsLateSlots = 3

// isPrefs reports whether msg is this node's own proposer-preferences partial.
func isPrefs(msg *spectypes.SignedSSVMessage) *spectypes.PartialSignatureMessages {
	body := partialBody(msg)
	if body == nil || body.Type != spectypes.ProposerPreferencesPartialSig || len(body.Messages) == 0 {
		return nil
	}
	return body
}

// prefs5Roots presents five distinct signing roots for one (slot, signer) against a budget of four,
// then repeats an accepted root (MSG-05 clauses 1-3, FLT-07).
//
// Honest receivers (message/validation/partial_validation.go validateDistinctRootBudget :391-415):
//   - row 0, the honest root: the first message for (P, signer), recorded with no budget check;
//   - rows 1-4, four clones with SigningRoot[0] ^= i, staggered 100 ms apart so the honest root is
//     always recorded first: rows 1-3 are accepted (2, 3, 4 roots), row 4 is IGNOREd
//     "proposer-preferences, 4 distinct root(s) seen and 1 new";
//   - row 5, the honest root again with PartialSignature[0] ^= 0xff (byte-distinct, so the gossip
//     seen-cache does not drop it), 2 s later: IGNOREd "proposer-preferences, no new signing root".
//     This is the same-peer repeat, which was a REJECT before 635f697b7 — the regression MSG-05
//     clause 3 guards.
//
// Every row keeps the honest payload slot and topic. The gas limit is unchanged: the roots are
// flipped directly, so MSG-05's "five gas-limit values" evidence item cannot be produced.
func prefs5Roots(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if isPrefs(msg) == nil {
		return identity
	}

	out := identity
	for i := 1; i <= 4; i++ {
		c, err := Clone(msg)
		if err != nil {
			return identity
		}
		body := partialBody(c)
		if body == nil {
			return identity
		}
		body.Messages[0].SigningRoot[0] ^= byte(i)
		if err := setPartialBody(c, body); err != nil {
			return identity
		}
		out = append(out, Outgoing{Msg: c, Slot: slot, Resign: true, Delay: time.Duration(i) * 100 * time.Millisecond})
	}

	repeat, ok := repeatOf(msg)
	if !ok {
		return identity
	}
	return append(out, Outgoing{Msg: repeat, Slot: slot, Resign: true, Delay: 2 * time.Second, Label: "repeat"})
}

// repeatOf returns a clone of msg with the same signing root and a perturbed first partial
// signature, so it is byte-distinct (the gossip seen-cache hashes the full SignedSSVMessage,
// network/topics/msg_id.go) but validation reads it as the same root. Validation never checks the
// inner BLS partial.
func repeatOf(msg *spectypes.SignedSSVMessage) (*spectypes.SignedSSVMessage, bool) {
	c, err := Clone(msg)
	if err != nil {
		return nil, false
	}
	body := partialBody(c)
	if body == nil || len(body.Messages) == 0 || len(body.Messages[0].PartialSignature) == 0 {
		return nil, false
	}
	body.Messages[0].PartialSignature[0] ^= 0xff
	if err := setPartialBody(c, body); err != nil {
		return nil, false
	}
	return c, true
}

// prefsEarly sends an extra copy whose payload slot is shifted 65 slots ahead of now (MSG-04). The
// target epoch is not fetched yet on any receiver, so validateBeaconDuty's RoleProposerPreferences
// assignment gate is skipped (common_checks.go:212-218) and the copy lands on ErrEarlySlotMessage
// ("message was sent before slot starts, got early by <d>", errors.go:75 + common_checks.go:33).
//
// Margin (addendum D.4). With now = 32*E0 + j and T = now + 65:
//   - j <= 30: epoch(T) = E0+2, the bound is the start of E0+1, early by (32 - j)*12 s - δ;
//   - j = 31: epoch(T) = E0+3, the bound is the start of E0+2, early by 33*12 s - δ = 396 s - δ;
//
// where δ is the in-slot send offset plus propagation. The scheduler's emission hold emits the next
// epoch at the first tick with slot%32 > 14 (operator/duties/proposer_preferences.go:95-127), so
// the steady state is j = 15: early by about 17 slots = 3m24s - δ. Only the first tick after a
// restart emits at another j. The tester does not pick the slot; the margin follows the emission
// tick. The old 64-slot rule logged about 12m59s, so the magnitude alone identifies the build.
// prefs-early-epoch probes the bound itself.
func prefsEarly(msg *spectypes.SignedSSVMessage, slot, now phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if isPrefs(msg) == nil {
		return identity
	}

	c, err := Clone(msg)
	if err != nil {
		return identity
	}
	body := partialBody(c)
	if body == nil {
		return identity
	}
	body.Slot = now + prefsEarlySlots
	if err := setPartialBody(c, body); err != nil {
		return identity
	}
	// The topic keeps following the original slot: pre-Boole both slots resolve to the same subnet,
	// and this keeps the fault about the slot in the payload, which is what validation reads.
	return append(identity, Outgoing{Msg: c, Slot: slot, Resign: true})
}

// prefsLate sends a copy that arrives 3 slots after the honest preference's own proposal slot
// (MSG-04), by DELAYING THE SEND rather than shifting the payload slot backward the way prefsEarly
// shifts it forward.
//
// TRAP 1 (do not "simplify" this back to a backdated payload slot): validateBeaconDuty's
// RoleProposerPreferences branch (message/validation/common_checks.go ~206) runs BEFORE
// validateSlotTime (~213), and rejects an unassigned slot in the current fetched-and-fresh epoch
// with ErrNoDuty. A payload slot of "now - 3" sits in that fetched epoch and is essentially never
// this validator's own proposal slot, so shifting the payload slot backward lands the copy on
// ErrNoDuty — the wrong rule — not lateness. Keeping the payload slot ON the honest proposal slot
// (unchanged from the identity message) makes the assignment gate pass, and the copy is instead
// delivered late by delaying the SEND.
//
// TRAP 2: an otherwise-identical copy of an already-accepted message carries the SAME signing root,
// and validateDistinctRootBudget — inside the `signerState != nil` limit block, which ALSO runs
// before validateSlotTime (partial_validation.go:227-247) — IGNOREs it first as
// "proposer-preferences, no new signing root". So the delayed copy must carry a second, distinct
// root, which the 4-root budget still admits, letting execution reach validateSlotTime, where
// messageLateness (ttl = LateSlotAllowance = 2, plus lateMessageMargin 3 s) fires: the copy
// arrives about 12 s + x - 3 s late. With the emission hold the send delay is 17-48 slots in
// steady state; the 67-slot role-8 ring and stateTTL keep P's state for that long.
func prefsLate(msg *spectypes.SignedSSVMessage, slot, now phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	body := isPrefs(msg)
	if body == nil {
		return identity
	}

	delaySlots := int(body.Slot) + prefsLateSlots - int(now)
	if delaySlots <= 0 {
		return identity
	}

	c, err := Clone(msg)
	if err != nil {
		return identity
	}
	cbody := partialBody(c)
	if cbody == nil || len(cbody.Messages) == 0 {
		return identity
	}
	// Distinct root #2 against the 4-root budget — see TRAP 2 above. The payload slot is left
	// untouched: it must stay the honest proposal slot for the assignment gate to pass.
	cbody.Messages[0].SigningRoot[0] ^= 0x01
	if err := setPartialBody(c, cbody); err != nil {
		return identity
	}
	// The topic keeps following the original slot, same as prefsEarly.
	return append(identity, Outgoing{Msg: c, Slot: slot, Resign: true, DelaySlots: delaySlots})
}

// prefsReplay repeats one valid preference at a high rate for 66 slots (FLT-11). The signing root is
// preserved — that is the rule under test — but the partial signature bytes are perturbed, because
// gossipsub suppresses a byte-identical duplicate before it ever leaves this node, and validation
// never inspects the partial signature.
//
// Expected outcome on the new base (addendum D.6): every copy passes the assignment gate and hits the
// distinct-root budget first, which runs before slot time, so the whole series is IGNOREd
// "proposer-preferences, no new signing root" — even copies sent after P+2 are classified as dedup,
// never "got late by". IGNORE carries no gossipsub p4 penalty: the flooder pays nothing in score.
// Nothing is recorded.
//
// This perturbation only covers the FIRST send in the series, deduping it against the honest
// original. It is NOT enough on its own: SignSSVMessage is deterministic, so re-signing the same
// bytes on every one of the replayCount further repeats would re-encode to the exact same
// SignedSSVMessage every time — the series would dedupe against ITSELF, not just against the
// honest message, and "sent" would count up to 15,841 publishes that gossipsub silently collapsed
// into roughly one. The decorator (faultnet.go sendAsync) closes that gap: it perturbs the same
// byte again before each repeat, so every send in the series is byte-distinct from every other.
func prefsReplay(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if isPrefs(msg) == nil {
		return identity
	}

	c, err := Clone(msg)
	if err != nil {
		return identity
	}
	body := partialBody(c)
	if body == nil {
		return identity
	}
	if len(body.Messages[0].PartialSignature) == 0 {
		return identity
	}
	body.Messages[0].PartialSignature[0] ^= 0xff
	if err := setPartialBody(c, body); err != nil {
		return identity
	}
	return append(identity, Outgoing{Msg: c, Slot: slot, Resign: true, Repeat: replayCount, Every: replayEvery})
}

// perturbForRepeat mutates msg's partial-signature bytes so the i-th repeat of a series (i > 0) is
// byte-distinct from every other send already made in it. Called by the decorator (faultnet.go
// sendAsync), never by Plan: Plan hands out one message to be repeated, and mutating it further on
// each iteration is part of "the sending", not the planning. The signing root is left untouched —
// that is the rule prefs-replay is testing — and validation never inspects the partial signature,
// so this cannot change which rule any given send lands on. A no-op (nil error) on anything that
// isn't a non-empty partial-signature message, so a future Repeat-using fault on an unexpected
// shape degrades to "no further perturbation" rather than failing the send outright.
//
// The counter is written across the first FOUR bytes (little-endian), not just the first byte:
// a single byte only spans 256 distinct values, so a series longer than 256 sends (prefs-replay
// runs 15,840) would alias every 256th iteration back onto a value already sent — and the seen-
// cache TTL (~385s) is long enough relative to the series' own duration that those aliased repeats
// collide with each other's still-live cache entries and get silently dropped by gossipsub, so
// "sent" would overcount what actually reached the wire. Four bytes give ~4 billion distinct values,
// far past any series this menu runs.
func perturbForRepeat(msg *spectypes.SignedSSVMessage, i int) error {
	body := partialBody(msg)
	if body == nil || len(body.Messages) == 0 || len(body.Messages[0].PartialSignature) < 4 {
		return nil
	}
	binary.LittleEndian.PutUint32(body.Messages[0].PartialSignature[:4], uint32(i)) // #nosec G115 -- i is bounded by replayCount (~15,840), well within uint32
	return setPartialBody(msg, body)
}

// slotsPerEpoch is valid only on 32-slot networks — ssv-mini, hoodi and mainnet all are, and the
// fault menu only ever runs on those.
const slotsPerEpoch = 32

// forgeConsensusMsg reuses this node's role-7 MessageID but carries a QBFT proposal, so the honest
// nodes hit the "this role has no consensus" rule (PTC-07, MSG-03).
func forgeConsensusMsg(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if role(msg) != spectypes.RolePTCAttester || partialBody(msg) == nil {
		return identity
	}

	msgID := msg.SSVMessage.GetID()
	qbftMsg := &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     specqbft.Height(slot),
		Round:      specqbft.FirstRound,
		Identifier: msgID[:],
		Root:       [32]byte{0x11},
	}
	data, err := qbftMsg.Encode()
	if err != nil {
		return identity
	}

	forged := &spectypes.SignedSSVMessage{
		OperatorIDs: msg.OperatorIDs,
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    data,
		},
	}
	return append(identity, Outgoing{Msg: forged, Slot: slot, Resign: true})
}

// dupEntry sends the honest role-7 partial plus a clone with two entries (MSG-03, PTC-07).
//
// The entry-count rule is structural: maxValidatorRoleSignatures (partial_validation.go:318-327)
// returns 1 for role 7, and the check runs in validatePartialSignatureMessageSemantics (:139-146),
// before any duty logic, signer state or message-limit check. So the honest partial goes out too:
// it cannot pre-empt the rule, and it keeps this operator's PTC contribution (in M3, f stays at 2
// with operators 4 and 6 already degraded). The duplicate entry keeps the same validator index, so
// the same-index rule (:160-169) passes and the count rule is what fires: REJECT "too many
// signatures in a partial-signature message, got 2, want 1" (errors.go:134). The old "per-signer
// limit fires first" rationale, and PTC-07's "got more partial signature messages..." oracle, are
// obsolete on this base; that text now means the fault is malformed.
func dupEntry(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if role(msg) != spectypes.RolePTCAttester {
		return identity
	}
	c, err := Clone(msg)
	if err != nil {
		return identity
	}
	body := partialBody(c)
	if body == nil || len(body.Messages) != 1 {
		return identity
	}
	body.Messages = append(body.Messages, body.Messages[0])
	if err := setPartialBody(c, body); err != nil {
		return identity
	}
	return append(identity, Outgoing{Msg: c, Slot: slot, Resign: true})
}

// ptcExtraSlots sends the honest PTC partial for slot S plus two forged copies for S+1 and S+2 of
// the same epoch — three PTC partials, at three different slots, in one epoch (MSG-07).
//
// What this actually exercises: the §7 PTC assignment gate, not the per-epoch duty-count rule. A
// validator holds exactly one PTC duty slot per epoch, so validateBeaconDuty's RolePTCAttester
// branch (message/validation/common_checks.go:237-243) refuses both forged copies with ErrNoDuty
// ("no duty for this epoch", errors.go:85, IGNORE) — it runs at partial_validation.go:223, BEFORE
// validateDutyCount at :251, so the per-epoch limit (dutyLimit role 7 returns 2,
// common_checks.go:138-142) is never reached for either copy (errata #120 stands; decision D5:
// MSG-07's count clause stays PARTIAL, no burst mode). PTC duties are now pre-fetched for the
// current and next epoch for every participating validator (operator/duties/ptc_attestation.go
// :229-260), so the "epoch not yet fetched" tolerance window is close to unreachable. The
// 2026-09-09 runner-layer observation ("no duty assigned", runner/error.go:22, errata #136) is
// unexplained on this base; record which string fires. What this DOES prove is that the honest side
// enforces the one-PTC-duty-per-epoch assignment.
//
// Why forward and delayed rather than backdated or immediate: RolePTCAttester is a monotonic-slot
// role (common_checks.go monotonicSlotRole) — once the honest message for slot S advances the
// signer's MaxSlot to S, a backdated copy for S-1 or S-2 is refused at the monotonic-slot check
// (ErrSlotAlreadyAdvanced) before it ever reaches the assignment gate this fault targets. Sending S+1
// and S+2 keeps MaxSlot advancing, so that check passes. And role 7 has no earliness allowance
// (common_checks.go earlySlotAllowance), so a copy for S+1 or S+2 sent immediately would be refused
// as early instead of reaching the assignment gate; DelaySlots (converted to a real delay by the
// decorator, which holds the network config Plan itself must stay free of) makes each copy arrive
// during its own slot instead.
func ptcExtraSlots(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if role(msg) != spectypes.RolePTCAttester || partialBody(msg) == nil {
		return identity
	}

	epochStart := slot - slot%slotsPerEpoch
	epochEnd := epochStart + slotsPerEpoch - 1
	out := identity
	for i := phase0.Slot(1); i <= 2; i++ {
		if slot+i > epochEnd {
			continue // no same-epoch room ahead of this slot yet
		}
		c, err := Clone(msg)
		if err != nil {
			continue
		}
		body := partialBody(c)
		if body == nil {
			continue
		}
		body.Slot = slot + i
		// Vary the bytes so gossipsub does not suppress the copy as a duplicate. Validation never
		// inspects the partial signature, so this does not change which rule the copy lands on.
		if len(body.Messages[0].PartialSignature) == 0 {
			continue
		}
		body.Messages[0].PartialSignature[0] ^= byte(i)
		if err := setPartialBody(c, body); err != nil {
			continue
		}
		out = append(out, Outgoing{Msg: c, Slot: slot, Resign: true, DelaySlots: int(i)})
	}
	if len(out) != 3 {
		return identity // all or nothing: two extra duties are what makes the third one the third
	}
	return out
}

// forgeGloasRoles clones ANY outgoing validator-role partial-signature message into the Gloas roles
// 7 and 8 at a fixed pre-fork slot (decision D3: no role-9 clone; RoleEnvelopeProposer no longer
// exists), which the role-at-slot gate must refuse regardless of when the
// forged messages are actually broadcast (MSG-02): validRoleAtSlot is the first check in partial
// semantics, ahead of the type-to-role matrix and ahead of all duty logic including the lateness
// rule, so a Gloas role at a pre-fork slot still lands on ErrInvalidRole even long after the fork.
//
// Triggering on ANY partial-signature message — not just validator-registration — makes this fault
// self-sufficient. It used to trigger only on an outgoing RoleValidatorRegistration partial, but
// post-fork there are none: every honest node (this one included, unless vr-postfork — the other
// MSG-02 half — is the active fault) stops emitting role-4 messages at the fork. The pass procedure
// waits for the fork before iterating the menu, so a VR-only trigger for role7-prefork would never
// fire in practice, leaving MSG-02 with zero coverage. Piggybacking on any partial-sig broadcast
// (PTC, preferences, committee, aggregator, ...) removes that dependency entirely.
//
// preForkSlot is computed by the decorator from the network config (GLOAS_FORK_EPOCH *
// SlotsPerEpoch, minus one) and passed in — Plan itself never reads a config.
//
// now is the decorator's own wall-clock slot (the same value Plan already receives as its `now`
// argument), used only for a sanity bound: GloasForkEpoch() reports ok for an unscheduled
// far-future epoch by design (networkconfig/beacon.go), and multiplying that sentinel epoch by
// SlotsPerEpoch wraps a uint64 into an arbitrary value — the decorator's own epoch==0 guard only
// covers one degenerate input, not this one. A genuine pre-fork slot is always comfortably below
// the current wall clock, so preForkSlot >= now can only mean the multiplication wrapped; forging
// against that slot would target nothing meaningful, so treat the fault as inapplicable instead.
//
// Volume: Plan forges on every trigger; the decorator throttles the value (budget.go) to one forged
// set per epoch and a small total cap, so the REJECTs stay under the peer-scoring threshold that
// got operator 5 graylisted in 13 minutes on 2026-09-09 (errata #139).
func forgeGloasRoles(msg *spectypes.SignedSSVMessage, slot, now, preForkSlot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if partialBody(msg) == nil {
		return identity
	}
	if preForkSlot >= now {
		return identity
	}
	// A committee-role partial's MsgID executor is 16 zero bytes plus the 32-byte committee ID, not
	// a validator pubkey. The clone below rewrites MsgID with a validator-role runner but keeps the
	// executor bytes as-is, so the publish path takes the validator branch and
	// ValidatorStore().Validator(executorID) fails to resolve a validator that was never encoded
	// there — nothing reaches the wire, and the injection log fires anyway (faults.Fired runs in
	// send, before the inner broadcast), leaving three "injected" lines and three failed-send
	// warnings per committee partial with no corresponding wire traffic. Even if it somehow
	// published, the honest side resolves the executor before the role check, so it would answer
	// unknown-validator rather than the ErrInvalidRole this fault targets. Skip both committee-
	// shaped roles (plain committee and the Gloas aggregator-committee combination) and let them
	// through untouched; validator-role partials (proposer, preferences, PTC, aggregator,
	// validator registration) remain the trigger.
	if r := role(msg); r == spectypes.RoleCommittee || r == spectypes.RoleAggregatorCommittee {
		return identity
	}

	var domain spectypes.DomainType
	copy(domain[:], msg.SSVMessage.GetID().GetDomain())
	var pk spectypes.ValidatorPK
	copy(pk[:], msg.SSVMessage.GetID().GetDutyExecutorID())

	out := identity
	for _, r := range []spectypes.RunnerRole{
		spectypes.RolePTCAttester,
		spectypes.RoleProposerPreferences,
	} {
		c, err := Clone(msg)
		if err != nil {
			continue
		}
		c.SSVMessage.MsgID = spectypes.NewValidatorMsgID(domain, pk, r)
		body := partialBody(c)
		if body == nil {
			continue
		}
		body.Slot = preForkSlot
		if err := setPartialBody(c, body); err != nil {
			continue
		}
		out = append(out, Outgoing{Msg: c, Slot: slot, Resign: true})
	}
	return out
}

// fabricateRoot returns base with byte 31 flipped by i. The §5 values that already flip byte 0
// (prefs-5-roots, prefs-late, prefs-early-epoch) keep to byte 0; the ones below keep to byte 31, so
// no two values' roots collide by construction.
func fabricateRoot(base [32]byte, i byte) [32]byte {
	base[31] ^= i
	return base
}

// clonePartial clones msg and applies mutate to its decoded partial-signature body. It reports
// false when any step fails, so a caller can fall back to identity.
func clonePartial(msg *spectypes.SignedSSVMessage, mutate func(*spectypes.PartialSignatureMessages)) (*spectypes.SignedSSVMessage, bool) {
	c, err := Clone(msg)
	if err != nil {
		return nil, false
	}
	body := partialBody(c)
	if body == nil || len(body.Messages) == 0 {
		return nil, false
	}
	mutate(body)
	if err := setPartialBody(c, body); err != nil {
		return nil, false
	}
	return c, true
}

// prefs34Apart probes MSG-06: the role-8 signer-state ring must hold a slot P and the slot P+34 at
// the same time (67-slot ring, message/validation/validation.go:435-440). Addendum D.2.
//
// The probe fires only when the honest emission for P is in the epoch before epoch(P) (the steady
// emission hold at j = 15) and k = P mod 32 <= 29, so that P+34 = 32*(E_P+1) + k + 2 lies in epoch
// E_P+1. Let d0 = 32*E_P - now (17 in steady state):
//
//	#  payload  root                  send                 honest verdict
//	0  P        r1 (honest)           now                  accept (first message, no budget check)
//	1  P        r1 ^ 1 (byte 31)      now + 100 ms         accept (2 roots)
//	2  P        r1 ^ 2                now + 200 ms         accept (3 roots)
//	3  P        r1 ^ 3                now + 300 ms         accept (4 roots)
//	4  P+34     q1 = r1 ^ 0x40        slot 1 of E_P        accept, new state at ring index (P+34)%67
//	5  P        r1 ^ 5                slot 2 of E_P        IGNORE "proposer-preferences, 4 distinct root(s) seen and 1 new"
//	6  P+34     q1, bytes perturbed   slot 3 of E_P        IGNORE "proposer-preferences, no new signing root"
//
// Row 4 must arrive after epoch(P) starts (P+34's earliness bound is the start of E_P) and before
// slot 15 of E_P (receivers fetch E_P+1's proposer duties from slot 15, so until then the
// assignment gate is skipped for P+34). On a 34-slot ring, row 4 evicts P and row 5 is accepted with
// no line; row 6 is positive proof that row 4 was recorded. The topic stays P for every row
// (pre-Boole: same subnet). Row 5 is inside P's lateness for every k, and the budget check precedes
// slot time anyway.
func prefs34Apart(msg *spectypes.SignedSSVMessage, slot, now phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	body := isPrefs(msg)
	if body == nil {
		return identity
	}
	p := body.Slot
	epochP := p / slotsPerEpoch
	if epochP != now/slotsPerEpoch+1 || p%slotsPerEpoch > 29 {
		return identity
	}
	d0 := int(epochP*slotsPerEpoch - now)

	flip := func(i byte, payload phase0.Slot) (*spectypes.SignedSSVMessage, bool) {
		return clonePartial(msg, func(b *spectypes.PartialSignatureMessages) {
			b.Slot = payload
			b.Messages[0].SigningRoot = fabricateRoot(b.Messages[0].SigningRoot, i)
		})
	}

	out := identity
	for i := 1; i <= 3; i++ {
		c, ok := flip(byte(i), p)
		if !ok {
			return identity
		}
		out = append(out, Outgoing{Msg: c, Slot: slot, Resign: true, Delay: time.Duration(i) * 100 * time.Millisecond, Label: "p"})
	}
	q1, ok := flip(0x40, p+34)
	if !ok {
		return identity
	}
	fifth, ok := flip(0x05, p)
	if !ok {
		return identity
	}
	q1Repeat, ok := repeatOf(q1)
	if !ok {
		return identity
	}
	return append(out,
		Outgoing{Msg: q1, Slot: slot, Resign: true, DelaySlots: d0 + 1, Label: "p+34"},
		Outgoing{Msg: fifth, Slot: slot, Resign: true, DelaySlots: d0 + 2, Label: "p-5th"},
		Outgoing{Msg: q1Repeat, Slot: slot, Resign: true, DelaySlots: d0 + 3, Label: "p+34-repeat"},
	)
}

// prefsEarlyEpoch probes the role-8 earliness bound at one slot (MSG-04 card step 8; addendum D.4).
// T = FirstSlotAtEpoch(epoch(now)+2) is the first slot past the bound: a preference for T is
// expected from the start of epoch(now)+1 (common_checks.go messageEarliness :52-65). Under the
// pre-48f47a1c1 64-slot rule T would be accepted, so this discriminates the rule, not just the
// magnitude. With now = 32*E0 + j:
//
//	probe          T, root[0] ^ 0x01, sent 31-j slots later (slot 31 of E0):
//	               IGNORE "message was sent before slot starts, got early by ~12 s - δ" (> 1.05 s)
//	control        T, root[0] ^ 0x02, sent 32-j slots later (slot 0 of E0+1, exactly the bound):
//	               accepted with no line — E0+2 is not fetched until slot 15, so the gate is skipped
//	control proof  the control root with perturbed bytes, 32-j slots + 2 s later:
//	               IGNORE "proposer-preferences, no new signing root" for slot T — positive evidence
//	               the control was recorded (acceptance itself logs nothing)
//
// The honest message is still sent. The topic follows the original slot.
func prefsEarlyEpoch(msg *spectypes.SignedSSVMessage, slot, now phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if isPrefs(msg) == nil {
		return identity
	}
	e0 := now / slotsPerEpoch
	j := int(now % slotsPerEpoch)
	target := (e0 + 2) * slotsPerEpoch

	flip := func(i byte) (*spectypes.SignedSSVMessage, bool) {
		return clonePartial(msg, func(b *spectypes.PartialSignatureMessages) {
			b.Slot = target
			b.Messages[0].SigningRoot[0] ^= i
		})
	}
	probe, ok := flip(0x01)
	if !ok {
		return identity
	}
	control, ok := flip(0x02)
	if !ok {
		return identity
	}
	proof, ok := repeatOf(control)
	if !ok {
		return identity
	}
	return append(identity,
		Outgoing{Msg: probe, Slot: slot, Resign: true, DelaySlots: 31 - j, Label: "probe"},
		Outgoing{Msg: control, Slot: slot, Resign: true, DelaySlots: 32 - j, Label: "control"},
		Outgoing{Msg: proof, Slot: slot, Resign: true, DelaySlots: 32 - j, Delay: 2 * time.Second, Label: "control-proof"},
	)
}

// forgePreForkRegistration serves vr-prefork-slot-replay (MSG-02 gate 3). It clones this node's
// outgoing role-7 partial — one per validator per epoch post-fork, with a validator pubkey as its
// executor — into a validator-registration partial stamped with preForkSlot:
// MsgID role 4, Type ValidatorRegistrationPartialSig, Slot preForkSlot, the same single entry.
//
// It fires only from wall-clock epoch fork+1 on, so the fork epoch's in-flight control (the last
// pre-fork registrations, still admitted) is never polluted, and only when preForkSlot < now (the
// far-future-fork wrap guard, as in forgeGloasRoles).
//
// Receiver path: topic and domain at the pre-fork slot are the same pre-Boole; validRoleAtSlot(4,
// preForkSlot) is true; one signer, type-role match, 1 entry, index in the committee; then
// registrationsRetired(receivedAt) (partial_validation.go:202-204, :308-311) IGNOREs it
// "validator registrations ended with the Gloas fork" (errors.go:93). That runs before the
// monotonic-slot check, which matters because role 4 is monotonic. No REJECT, so no peer-score
// cost; the decorator still throttles it to one clone per epoch so the count is readable.
func forgePreForkRegistration(msg *spectypes.SignedSSVMessage, slot, now, preForkSlot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if role(msg) != spectypes.RolePTCAttester {
		return identity
	}
	if body := partialBody(msg); body == nil || len(body.Messages) != 1 {
		return identity
	}
	if preForkSlot >= now || now/slotsPerEpoch < (preForkSlot+1)/slotsPerEpoch+1 {
		return identity
	}

	var domain spectypes.DomainType
	copy(domain[:], msg.SSVMessage.GetID().GetDomain())
	var pk spectypes.ValidatorPK
	copy(pk[:], msg.SSVMessage.GetID().GetDutyExecutorID())

	c, ok := clonePartial(msg, func(b *spectypes.PartialSignatureMessages) {
		b.Type = spectypes.ValidatorRegistrationPartialSig
		b.Slot = preForkSlot
	})
	if !ok {
		return identity
	}
	c.SSVMessage.MsgID = spectypes.NewValidatorMsgID(domain, pk, spectypes.RoleValidatorRegistration)
	return append(identity, Outgoing{Msg: c, Slot: slot, Resign: true})
}
