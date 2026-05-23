package twoab

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	wire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

// DispatchEnvelope routes a parsed 2abOBFT *wire.Envelope to the appropriate
// Process / HandlePeer method on the Controller / Scheduler. It is the inbound
// counterpart to the resolve loop's outbound broadcast — the production
// stand-in for the smoke bus's inline switch.
//
// `observedOffset` is the message's first-observation time relative to slot
// start; only KindPhase1Bundle consumes it (the bundle acceptance-window /
// diagnostics). Pass a zero duration for the other kinds.
//
// If the targeted slot has no active instance yet (peers broadcast before this
// operator's pre-consensus completes and StartNewInstance runs), the envelope
// is buffered on the Controller and replayed by Scheduler.DrainPending after
// StartNewInstance — preventing silent drops. Unlike bare OBFT this buffers the
// Phase-2a kinds (KindValue / KindNoValue) too.
//
// Returns an error if the envelope's Kind is unknown or if the underlying
// Process call fails for reasons other than missing instance.
//
// Cross-variant safety: this only ever sees 2abOBFT envelopes — wire.Unwrap
// (in DispatchBytes) rejects bare-OBFT bytes via the ProtocolTag mismatch
// before an Envelope is ever produced.
func DispatchEnvelope(ctx context.Context, sched *Scheduler, env *wire.Envelope, observedOffset time.Duration) error {
	if sched == nil {
		return errors.New("twoab adapter: nil Scheduler")
	}
	if env == nil {
		return errors.New("twoab adapter: nil Envelope")
	}
	ctrl := sched.Controller()
	switch env.Kind {
	case wire.KindPhase1Bundle:
		if env.Phase1Bundle == nil {
			return errors.New("twoab adapter: KindPhase1Bundle envelope with nil bundle")
		}
		err := sched.HandlePeerPhase1Bundle(ctx, env.Phase1Bundle, observedOffset)
		if errors.Is(err, ErrNoActiveInstance) {
			ctrl.BufferEnvelope(phase0.Slot(env.Phase1Bundle.Height), PendingEnvelope{
				Bundle: env.Phase1Bundle, ObservedOffset: observedOffset,
			})
			return nil
		}
		return err
	case wire.KindValue:
		if env.ValueMsg == nil {
			return errors.New("twoab adapter: KindValue envelope with nil ValueMsg")
		}
		err := ctrl.ProcessValueMsg(env.ValueMsg)
		if errors.Is(err, ErrNoActiveInstance) {
			ctrl.BufferEnvelope(phase0.Slot(env.ValueMsg.Height), PendingEnvelope{
				ValueMsg: env.ValueMsg,
			})
			return nil
		}
		return err
	case wire.KindNoValue:
		if env.NoValueMsg == nil {
			return errors.New("twoab adapter: KindNoValue envelope with nil NoValueMsg")
		}
		err := ctrl.ProcessNoValueMsg(env.NoValueMsg)
		if errors.Is(err, ErrNoActiveInstance) {
			ctrl.BufferEnvelope(phase0.Slot(env.NoValueMsg.Height), PendingEnvelope{
				NoValueMsg: env.NoValueMsg,
			})
			return nil
		}
		return err
	case wire.KindCommit:
		if env.Commit == nil {
			return errors.New("twoab adapter: KindCommit envelope with nil Commit")
		}
		err := ctrl.ProcessCommit(env.Commit)
		if errors.Is(err, ErrNoActiveInstance) {
			ctrl.BufferEnvelope(phase0.Slot(env.Commit.Height), PendingEnvelope{
				Commit: env.Commit,
			})
			return nil
		}
		return err
	case wire.KindCertificate:
		if env.Certificate == nil {
			return errors.New("twoab adapter: KindCertificate envelope with nil Certificate")
		}
		err := ctrl.ProcessCertificate(env.Certificate)
		if errors.Is(err, ErrNoActiveInstance) {
			ctrl.BufferEnvelope(phase0.Slot(env.Certificate.Height), PendingEnvelope{
				Certificate: env.Certificate,
			})
			return nil
		}
		return err
	default:
		return fmt.Errorf("twoab adapter: unknown envelope kind 0x%02x", byte(env.Kind))
	}
}

// DispatchBytes parses raw 2abOBFT envelope bytes and dispatches the result.
// wire.Unwrap rejects non-2abOBFT framing (wrong ProtocolTag / version /
// unknown kind), so a bare-OBFT envelope fed here errors at unwrap rather than
// being misrouted.
func DispatchBytes(ctx context.Context, sched *Scheduler, data []byte, observedOffset time.Duration) error {
	env, err := wire.Unwrap(data)
	if err != nil {
		return fmt.Errorf("twoab adapter: unwrap envelope: %w", err)
	}
	return DispatchEnvelope(ctx, sched, env, observedOffset)
}
