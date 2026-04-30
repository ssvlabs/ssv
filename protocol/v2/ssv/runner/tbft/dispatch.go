package tbft

import (
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/tbft/wire"
)

// Dispatch helpers for the receive path.
//
// The runner's network/validation layer parses an incoming
// `*spectypes.SignedSSVMessage`, extracts the `Data` payload, and hands
// it to one of these functions to route into the right Controller method.
// The actual SSV-message-envelope parsing (operator-key signature
// verification, MessageID matching) lives at the runner level — the
// helpers here assume the caller has already authenticated the sender
// and is now ready to deliver the inner TBFT message.

// DispatchEnvelope routes a parsed `*wire.Envelope` to the appropriate
// `Process*` method on the Controller based on its Kind.
//
// Returns an error if the envelope's Kind is unknown or if the underlying
// Process call fails (e.g. routing to a slot with no active instance).
func DispatchEnvelope(c *Controller, env *wire.Envelope) error {
	if c == nil {
		return errors.New("tbft adapter: nil Controller")
	}
	if env == nil {
		return errors.New("tbft adapter: nil Envelope")
	}
	switch env.Kind {
	case wire.KindOnion:
		return c.ProcessOnion(env.Onion)
	case wire.KindNonReceipt:
		return c.ProcessNonReceipt(env.NonReceipt)
	case wire.KindCandidate:
		return c.ProcessCandidate(env.Candidate)
	default:
		return fmt.Errorf("tbft adapter: unknown envelope kind 0x%02x", byte(env.Kind))
	}
}

// DispatchBytes is a convenience wrapper that parses raw envelope bytes
// (as produced by `wire.WrapOnion` / `wire.WrapNonReceipt` /
// `wire.WrapCandidate`) and dispatches the result to `c`.
//
// Returns an error if the bytes cannot be parsed as a valid TBFT envelope
// or if dispatch fails.
func DispatchBytes(c *Controller, data []byte) error {
	env, err := wire.Unwrap(data)
	if err != nil {
		return fmt.Errorf("tbft adapter: unwrap envelope: %w", err)
	}
	return DispatchEnvelope(c, env)
}
