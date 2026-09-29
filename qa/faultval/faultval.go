// Package faultval decorates the pubsub message validator so a faulted operator's own forged
// messages reach the mesh instead of being refused by its own validation gate.
//
// It exists because of a concrete gap the 2026-09-09 M3 ladder exposed. Six of the nineteen menu
// values had their rule fire ONLY on the faulted operator, with all three honest operators silent:
//
//	prefs-early    11/11        ptc-qbft        20/20
//	prefs-late     15/15        two-entries     19/19
//	prefs-replay   75,512       role7-prefork   3006/3009
//
// For each of those, docs/qa-glamsterdam-test-plan-passes.md §6.6 states the oracle as an
// "Expected result on the honest operators" — and there was nothing on the honest operators to
// read. The rule was proven enforced at the SENDER; the receivers were never tested. Wrapping the
// sender's gate turns those six into real honest-side evidence.
//
// This is a MODIFIER (FAULT_NO_LOCAL_VALIDATION), not a 20th menu value: FAULT selects exactly one
// misbehaviour, so a menu value would force a choice between prefs-early and observing the honest
// reaction to prefs-early. See qa/faults for that reasoning.
package faultval

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/qa/faults"
)

// acceptAll satisfies validation.MessageValidator by accepting unconditionally.
//
// It keeps `inner` so the decorator is a strict superset of the real validator: nothing is lost,
// and a future variant that wants to consult the real verdict (accept-own-only, or accept-but-log)
// has it to hand without changing any call site.
type acceptAll struct {
	inner  validation.MessageValidator
	logger *zap.Logger
}

// Validate accepts every message without consulting the real validator.
//
// It deliberately does NOT log per message. `role7-prefork` produced 3009 forged messages in a
// 13-minute window and `prefs-replay` produced 63,364; a line each would swamp the very log buffer
// the M3 oracles are read from, which is the same reason qa/faultnet keeps its repeat series
// silent (see qa/FAULTS.md §6). The boot banner records that the gate is off — that is the
// evidence a reader needs, and it cannot be missed.
func (a acceptAll) Validate(_ context.Context, _ peer.ID, _ *pubsub.Message) pubsub.ValidationResult {
	return pubsub.ValidationAccept
}

// ValidatorForTopic returns the same accept-all function for every topic.
// SetSelfPID forwards to the wrapped validator so its own-message recognition keeps working.
func (a acceptAll) SetSelfPID(pid peer.ID) { a.inner.SetSelfPID(pid) }

func (a acceptAll) ValidatorForTopic(string) func(context.Context, peer.ID, *pubsub.Message) pubsub.ValidationResult {
	return a.Validate
}

// Wrap returns mv unchanged unless the FAULT_NO_LOCAL_VALIDATION modifier is set, in which case it
// returns an accept-all validator.
//
// Call this ONLY on the pubsub gate (cli/operator/node.go's cfg.P2pNetworkConfig.MessageValidator).
// Do not wrap the runner's copy (valOpts.MessageValidator): runner-level rejections such as
// "no duty assigned" (ptc-3-per-epoch, MSG-07) and "no builders configured" (auth-no-builders,
// MSG-10) are real M3 oracles that must keep firing.
//
// When the modifier is off this is a no-op returning the identical interface value, so a
// FAULT_NO_LOCAL_VALIDATION-unset node is byte-identical in behaviour to one built without this
// package — the same property faults.None guarantees for the menu itself.
func Wrap(mv validation.MessageValidator, logger *zap.Logger) validation.MessageValidator {
	if !faults.NoLocalValidation() {
		return mv
	}
	return acceptAll{inner: mv, logger: logger}
}
