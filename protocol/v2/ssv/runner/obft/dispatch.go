package obft

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/obft/wire"
)

// DispatchEnvelope routes a parsed *wire.Envelope to the appropriate
// Process / Observe / HandlePeer method on the Controller / Scheduler.
//
// `observedOffset` is the bundle's first-observation time relative to slot
// start (used by Phase-1 bundle acceptance window check). Pass a zero
// duration for kinds where the receiver acceptance window doesn't apply
// (Commit / Certificate); only Phase1Bundle uses it.
//
// If the targeted slot has no active instance yet (e.g. peers broadcast
// before this operator's pre-consensus completes and StartNewInstance is
// called), the envelope is buffered on the Controller and replayed by
// Scheduler.DrainPending after StartNewInstance — preventing silent drops.
//
// Returns an error if the envelope's Kind is unknown or if the underlying
// Process call fails for reasons other than missing instance.
func DispatchEnvelope(ctx context.Context, sched *Scheduler, env *wire.Envelope, observedOffset time.Duration) error {
	if sched == nil {
		return errors.New("obft adapter: nil Scheduler")
	}
	if env == nil {
		return errors.New("obft adapter: nil Envelope")
	}
	ctrl := sched.Controller()
	switch env.Kind {
	case wire.KindPhase1Bundle:
		err := sched.HandlePeerPhase1Bundle(ctx, env.Phase1Bundle, observedOffset)
		if errors.Is(err, ErrNoActiveInstance) {
			ctrl.BufferEnvelope(phase0.Slot(env.Phase1Bundle.Height), PendingEnvelope{
				Bundle: env.Phase1Bundle, ObservedOffset: observedOffset,
			})
			return nil
		}
		return err
	case wire.KindCommit:
		err := ctrl.ProcessCommit(env.Commit)
		if errors.Is(err, ErrNoActiveInstance) {
			ctrl.BufferEnvelope(phase0.Slot(env.Commit.Height), PendingEnvelope{
				Commit: env.Commit,
			})
			return nil
		}
		return err
	case wire.KindCertificate:
		err := ctrl.ProcessCertificate(env.Certificate)
		if errors.Is(err, ErrNoActiveInstance) {
			ctrl.BufferEnvelope(phase0.Slot(env.Certificate.Height), PendingEnvelope{
				Certificate: env.Certificate,
			})
			return nil
		}
		return err
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
