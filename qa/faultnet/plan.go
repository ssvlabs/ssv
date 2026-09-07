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
// BroadcastAtSlot, which selects the topic; now is the current wall-clock slot. Plan is pure: it
// never signs, never sends, and never reads the clock, so every fault is table-testable.
func Plan(f faults.Fault, msg *spectypes.SignedSSVMessage, slot, now phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}

	if f == faults.None || msg == nil || msg.SSVMessage == nil {
		return identity
	}

	switch f {
	case faults.Prefs5Roots:
		return prefs5Roots(msg, slot)
	case faults.PrefsEarly:
		return prefsShift(msg, slot, now, true)
	case faults.PrefsLate:
		return prefsShift(msg, slot, now, false)
	case faults.PrefsReplay:
		return prefsReplay(msg, slot)
	case faults.PTCQBFT:
		return forgeConsensusMsg(msg, slot)
	case faults.TwoEntries:
		return dupEntry(msg, slot)
	case faults.PTC3PerEpoch:
		return ptcExtraSlots(msg, slot)
	case faults.Role7PreFork:
		return forgeGloasRoles(msg, slot)
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

// prefsShift sends an extra copy whose payload slot is offset from the current slot, so the honest
// message still reaches quorum while the copy proves the earliness or lateness reason (MSG-04).
func prefsShift(msg *spectypes.SignedSSVMessage, slot, now phase0.Slot, ahead bool) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if isPrefs(msg) == nil {
		return identity
	}

	var target phase0.Slot
	if ahead {
		target = now + prefsEarlySlots
	} else {
		if now < prefsLateSlots {
			return identity
		}
		target = now - prefsLateSlots
	}

	c, err := Clone(msg)
	if err != nil {
		return identity
	}
	body := partialBody(c)
	if body == nil {
		return identity
	}
	body.Slot = target
	if err := setPartialBody(c, body); err != nil {
		return identity
	}
	// The topic keeps following the original slot: pre-Boole both slots resolve to the same subnet,
	// and this keeps the fault about the slot in the payload, which is what validation reads.
	return append(identity, Outgoing{Msg: c, Slot: slot, Resign: true})
}

// prefsReplay repeats one valid preference at a high rate for 66 slots (FLT-11). The signing root is
// preserved — that is the rule under test — but the partial signature bytes are perturbed, because
// gossipsub suppresses a byte-identical duplicate before it ever leaves this node, and validation
// never inspects the partial signature.
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

// slotsPerEpoch is the mainnet and ssv-mini value; the fault menu only runs on those.
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

// ptcExtraSlots sends the same PTC partial for two more slots of the same epoch, taking the signer
// to three PTC duties in one epoch against a limit of two (MSG-07).
//
// The extra copies go FORWARD in time, each delayed to arrive during its own slot — not backdated.
// RolePTCAttester is a monotonic-slot role (message/validation/common_checks.go monotonicSlotRole):
// once the honest message for slot S advances the signer's MaxSlot to S, a backdated copy for S-1 or
// S-2 is refused at the monotonic-slot check (ErrSlotAlreadyAdvanced) before validateDutyCount is
// ever reached. Slots S+1 and S+2 keep MaxSlot advancing, so that check passes; arriving during S+1
// and S+2 respectively (via DelaySlots, converted to a real delay by the decorator, which holds the
// network config Plan itself must stay free of) also satisfies role 7's zero earliness allowance, so
// neither copy is early. That leaves three distinct duty slots signed in one epoch, which is what
// exceeds the limit of two.
func ptcExtraSlots(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if role(msg) != spectypes.RolePTCAttester || partialBody(msg) == nil {
		return identity
	}

	epochStart := slot - slot%slotsPerEpoch
	epochEnd := epochStart + slotsPerEpoch - 1
	out := []Outgoing{{Msg: msg, Slot: slot}}
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

// forgeGloasRoles clones a pre-fork validator-registration partial into the three Gloas roles, which
// the role-at-slot gate must refuse before the fork (MSG-02). Role 4 messages exist only pre-fork,
// so no fork check is needed here.
func forgeGloasRoles(msg *spectypes.SignedSSVMessage, slot phase0.Slot) []Outgoing {
	identity := []Outgoing{{Msg: msg, Slot: slot}}
	if role(msg) != spectypes.RoleValidatorRegistration || partialBody(msg) == nil {
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
		out = append(out, Outgoing{Msg: c, Slot: slot, Resign: true})
	}
	return out
}
