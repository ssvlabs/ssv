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
	// Tasks 7, 8 and 10 add their cases here.
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
