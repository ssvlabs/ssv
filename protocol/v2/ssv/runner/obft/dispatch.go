package obft

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/obft/wire"
)

// DispatchEnvelope routes a parsed *wire.Envelope to the appropriate
// Process / Observe / HandlePeer method on the Controller / Scheduler.
//
// `observedOffset` is the bundle's first-observation time relative to slot
// start (used by Phase-1 bundle acceptance window check). Pass a zero
// duration for kinds where the receiver acceptance window doesn't apply
// (Onion / NR / Certificate); only Phase1Bundle uses it.
//
// Returns an error if the envelope's Kind is unknown or if the underlying
// Process call fails (e.g. routing to a slot with no active instance).
func DispatchEnvelope(ctx context.Context, sched *Scheduler, env *wire.Envelope, observedOffset time.Duration) error {
	if sched == nil {
		return errors.New("obft adapter: nil Scheduler")
	}
	if env == nil {
		return errors.New("obft adapter: nil Envelope")
	}
	switch env.Kind {
	case wire.KindPhase1Bundle:
		return sched.HandlePeerPhase1Bundle(ctx, env.Phase1Bundle, observedOffset)
	case wire.KindOnion:
		return sched.Controller().ProcessOnion(env.Onion)
	case wire.KindNR:
		return sched.Controller().ProcessNR(env.NR)
	case wire.KindCertificate:
		return sched.Controller().ProcessCertificate(env.Certificate)
	default:
		return fmt.Errorf("obft adapter: unknown envelope kind 0x%02x", byte(env.Kind))
	}
}

// DispatchBytes is a convenience wrapper that parses raw envelope bytes
// and dispatches the result.
func DispatchBytes(ctx context.Context, sched *Scheduler, data []byte, observedOffset time.Duration) error {
	env, err := wire.Unwrap(data)
	if err != nil {
		return fmt.Errorf("obft adapter: unwrap envelope: %w", err)
	}
	return DispatchEnvelope(ctx, sched, env, observedOffset)
}
