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
}

// Plan returns what to send in place of msg. slot is the slot the caller passed to
// BroadcastAtSlot, which selects the topic; now is the current wall-clock slot; preForkSlot is a
// slot below the Gloas fork boundary, used only by role7-prefork. Plan is pure: it never signs,
// never sends, and never reads the clock or a network config, so every fault is table-testable —
// preForkSlot is computed by the decorator (which already holds the network config) and handed in,
// the same way DelaySlots is converted to a duration by the decorator rather than by Plan.
func Plan(f faults.Fault, msg *spectypes.SignedSSVMessage, slot, now, preForkSlot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}

	if f == faults.None || msg == nil || msg.SSVMessage == nil {
		return identity
	}

	switch f {
	case faults.Prefs5Roots:
		return prefs5Roots(msg, slot)
	case faults.PrefsEarly:
		return prefsEarly(msg, slot, now)
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
		return forgeGloasRoles(msg, slot, preForkSlot)
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

// prefsEarlySlots is one slot past the 64-slot preference lookahead allowance
// (message/validation/const.go proposerPreferencesEarlyEpochs = 2 epochs).
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

// prefs5Roots presents five distinct signing roots for one (slot, signer) against a budget of four
// (MSG-05, FLT-07).
func prefs5Roots(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	if isPrefs(msg) == nil {
		return []Outgoing{{Msg: msg, Slot: slot}}
	}

	out := []Outgoing{{Msg: msg, Slot: slot}}
	for i := 1; i <= 4; i++ {
		c, err := Clone(msg)
		if err != nil {
			continue
		}
		body := partialBody(c)
		if body == nil {
			continue
		}
		body.Messages[0].SigningRoot[0] ^= byte(i)
		if err := setPartialBody(c, body); err != nil {
			continue
		}
		out = append(out, Outgoing{Msg: c, Slot: slot, Resign: true})
	}
	return out
}

// prefsEarly sends an extra copy whose payload slot is shifted 65 slots ahead of now, so its target
// epoch sits beyond the 2-epoch proposer lookahead and validateBeaconDuty's RoleProposerPreferences
// assignment gate is skipped altogether (the epoch is not yet fetched) — landing the copy on
// ErrEarlySlotMessage instead (MSG-04). Confirmed correct as-is; only prefs-late needed a fix (see
// prefsLate's doc comment for why the two cannot share one implementation).
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
// before validateSlotTime — refuses a same-peer-same-root repeat first, landing on "duplicate
// signing root from peer" instead. So the delayed copy must carry a second, distinct root (as
// prefs5Roots does for its five), which the 4-root budget still admits, letting execution reach
// validateSlotTime, where messageLateness (ttl = LateSlotAllowance = 2) fires at proposal slot + 3.
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

// dupEntry puts two entries in a role-7 container, which the count rule allows only for committee
// roles (MSG-03).
//
// It sends ONLY the forged clone, never the honest original: validatePartialSigMessagesByDutyLogic
// checks validatePartialSignatureMessageLimit (the "already have a pre-consensus message for this
// signer+slot" rule) before it ever reaches the entry-count rule this fault targets, but only when
// a signerState already exists for that (signer, slot). Sending the honest message first would
// create that state and make the honest side reject the forgery on the wrong rule
// (ErrTooManyPartialSigMessage) instead of the one under test
// (ErrTooManySignaturesInPartialSigMessage). With no signerState yet, validateSlotTime and
// validateDutyCount pass and the entry-count check at the bottom of
// validatePartialSigMessagesByDutyLogic is what fires. The cost is that this operator contributes no
// honest PTC partial for the slot; pass M3 runs at size 7 (f=2), so quorum still forms from the other
// six operators, and MSG-03's oracle is read on the honest side regardless.
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
	return []Outgoing{{Msg: c, Slot: slot, Resign: true}}
}

// ptcExtraSlots sends the honest PTC partial for slot S plus two forged copies for S+1 and S+2 of
// the same epoch — three PTC partials, at three different slots, in one epoch (MSG-07).
//
// What this actually exercises: the §7 PTC assignment gate, not the per-epoch duty-count rule. A
// validator holds exactly one PTC duty slot per epoch, so validateBeaconDuty's RolePTCAttester
// branch (message/validation/common_checks.go:238) refuses both forged copies with ErrNoDuty — it
// runs at message/validation/partial_validation.go:189, BEFORE validateDutyCount at
// partial_validation.go:217, so the per-epoch limit check is never reached for either copy.
// ErrTooManyDutiesPerEpoch for role 7 is reachable only across a genuine duty re-fetch (the limit
// exists as "one duty per epoch plus a reorg margin", common_checks.go:131-136) — no sender-side
// shape can trigger that, so this fault cannot exercise it. What it DOES prove is that the honest
// side enforces the one-PTC-duty-per-epoch assignment correctly.
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

// forgeGloasRoles clones ANY outgoing partial-signature message into the three Gloas roles (7, 8,
// 9) at a fixed pre-fork slot, which the role-at-slot gate must refuse regardless of when the
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
func forgeGloasRoles(msg *spectypes.SignedSSVMessage, slot, preForkSlot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if partialBody(msg) == nil {
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
		spectypes.RoleEnvelopeProposer,
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
