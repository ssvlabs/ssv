package obft

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/wire"
)

// LifecycleHooks are callbacks the runner provides to the Scheduler. The
// runner controls *when* phases fire; the Scheduler executes the protocol
// mechanics inside each phase and calls the appropriate hook for runner
// concerns (fetching, broadcasting, submitting).
type LifecycleHooks struct {
	// FetchCandidate is invoked when the local operator is the designated
	// leader for `layer` of `slot` and the layer's `FetchAt` has arrived.
	// Returns the candidate-value bytes the leader signs and broadcasts.
	//
	// Required.
	FetchCandidate func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error)

	// HostValidate is invoked at receiver-side after a Phase-1 bundle has
	// been observed, to obtain the host application's valid/not-valid
	// verdict on the bundled candidate value. Returns (valid bool, err).
	//
	// Required. Implementors should run a stable head snapshot validation
	// (parent_root match, fork/domain, etc.) and lock the verdict for the
	// remainder of the slot.
	HostValidate func(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error)

	// Broadcast is invoked to gossip an OBFT wire-envelope-wrapped message
	// (Phase1Bundle / Onion / NR / Certificate) to peer operators.
	//
	// Required.
	Broadcast func(ctx context.Context, slot phase0.Slot, data []byte) error

	// SubmitOutput is invoked when Resolve produces a signed Output.
	//
	// Required.
	SubmitOutput func(ctx context.Context, slot phase0.Slot, output *obftcore.Output) error

	// BroadcastCertificate is invoked to gossip a final Certificate after
	// Resolve succeeds. Optional; if nil, the certificate is not gossiped
	// (the runner submits its own Output but doesn't help peers without
	// local reconstruction).
	BroadcastCertificate func(ctx context.Context, slot phase0.Slot, data []byte) error

	// OnMissedSlot is invoked when Resolve returns ErrNoQuorum or a related
	// failure. Optional.
	OnMissedSlot func(ctx context.Context, slot phase0.Slot, reason error)
}

func (h *LifecycleHooks) validate() error {
	if h == nil {
		return errors.New("obft adapter: nil LifecycleHooks")
	}
	if h.FetchCandidate == nil {
		return errors.New("obft adapter: LifecycleHooks.FetchCandidate is required")
	}
	if h.HostValidate == nil {
		return errors.New("obft adapter: LifecycleHooks.HostValidate is required")
	}
	if h.Broadcast == nil {
		return errors.New("obft adapter: LifecycleHooks.Broadcast is required")
	}
	if h.SubmitOutput == nil {
		return errors.New("obft adapter: LifecycleHooks.SubmitOutput is required")
	}
	return nil
}

// Scheduler combines a Controller with LifecycleHooks to expose per-slot
// phase mechanics as procedural methods. The runner calls these at the
// right slot offsets — the Scheduler does NOT manage time itself.
type Scheduler struct {
	controller *Controller
	hooks      *LifecycleHooks
}

// NewScheduler constructs a Scheduler.
func NewScheduler(controller *Controller, hooks *LifecycleHooks) (*Scheduler, error) {
	if controller == nil {
		return nil, errors.New("obft adapter: nil Controller")
	}
	if err := hooks.validate(); err != nil {
		return nil, err
	}
	return &Scheduler{controller: controller, hooks: hooks}, nil
}

// Controller returns the underlying Controller.
func (s *Scheduler) Controller() *Controller {
	return s.controller
}

// FetchAndBroadcastBundle executes Phase 1 for one layer where the local
// operator is the designated leader:
//
//  1. Call hooks.FetchCandidate to obtain the candidate value.
//  2. Build the Phase-1 bundle (locks σ-V on the value via EKM-style
//     enforcement; self-observes into the σ-pool).
//  3. Apply host validity verdict at the leader's own instance (host
//     validated the value at fetch time).
//  4. Broadcast the bundle via hooks.Broadcast.
func (s *Scheduler) FetchAndBroadcastBundle(ctx context.Context, slot phase0.Slot, layer int) error {
	value, err := s.hooks.FetchCandidate(ctx, slot, layer)
	if err != nil {
		return fmt.Errorf("obft scheduler: fetch candidate (slot=%d layer=%d): %w", slot, layer, err)
	}

	bundle, err := s.controller.BuildPhase1Bundle(slot, layer, value)
	if err != nil {
		return fmt.Errorf("obft scheduler: build phase-1 bundle: %w", err)
	}
	if err := s.controller.ApplyHostValidity(slot, layer, value, true); err != nil {
		return fmt.Errorf("obft scheduler: apply host validity (own bundle): %w", err)
	}

	data, err := wire.WrapPhase1Bundle(bundle)
	if err != nil {
		return fmt.Errorf("obft scheduler: wrap phase-1 bundle: %w", err)
	}
	if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
		return fmt.Errorf("obft scheduler: broadcast phase-1 bundle: %w", err)
	}
	return nil
}

// HandlePeerPhase1Bundle records a peer's Phase-1 bundle and runs host
// validity for the candidate value. Called by the dispatch layer when a
// KindPhase1Bundle envelope arrives from the network.
func (s *Scheduler) HandlePeerPhase1Bundle(ctx context.Context, b *obftcore.Phase1Bundle, observedOffset time.Duration) error {
	if b == nil {
		return errors.New("obft scheduler: nil phase-1 bundle")
	}
	if err := s.controller.ObservePhase1Bundle(b, observedOffset); err != nil {
		return fmt.Errorf("obft scheduler: observe phase-1 bundle: %w", err)
	}
	valid, err := s.hooks.HostValidate(ctx, phase0.Slot(b.Height), b.Layer, []byte(b.Value))
	if err != nil {
		return fmt.Errorf("obft scheduler: host validate (slot=%d layer=%d): %w", b.Height, b.Layer, err)
	}
	if err := s.controller.ApplyHostValidity(phase0.Slot(b.Height), b.Layer, []byte(b.Value), valid); err != nil {
		return fmt.Errorf("obft scheduler: apply host validity: %w", err)
	}
	return nil
}

// BuildAndBroadcastOnion executes the Phase-2 σ-emit step. Returns the
// Onion that was broadcast (so the caller may inspect it for tests).
//
// May be called multiple times during Phase 2: the first call at TCommit
// emits initial σ-state; subsequent calls (e.g., after late re-flood
// resolves Defer-partition for some layer) emit fresh σ partials. Gossipsub
// dedups identical bytes naturally; receivers' Instance dedups by content.
func (s *Scheduler) BuildAndBroadcastOnion(ctx context.Context, slot phase0.Slot) (*obftcore.Onion, error) {
	o, err := s.controller.BuildOwnOnion(slot)
	if err != nil {
		return nil, fmt.Errorf("obft scheduler: build own onion: %w", err)
	}
	data, err := wire.WrapOnion(o)
	if err != nil {
		return nil, fmt.Errorf("obft scheduler: wrap onion: %w", err)
	}
	if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
		return nil, fmt.Errorf("obft scheduler: broadcast onion: %w", err)
	}
	return o, nil
}

// PhaseTwoEndAndBroadcastNR applies the end-of-Phase-2 force-commit and
// broadcasts the local operator's KindNR. Call exactly once at TCommit +
// Delta2.
//
// Returns the NR for inspection; broadcasts it as a side effect.
//
// Also re-broadcasts an Onion if PhaseTwoEnd transitioned any Defer-partition
// layer to σ (cached partial entered the Onion via i.ownPartials).
func (s *Scheduler) PhaseTwoEndAndBroadcastNR(ctx context.Context, slot phase0.Slot) (*obftcore.NR, error) {
	if err := s.controller.PhaseTwoEnd(slot); err != nil {
		return nil, fmt.Errorf("obft scheduler: phase-2 end: %w", err)
	}
	// Re-emit Onion in case PhaseTwoEnd transitioned any Defer-partition
	// layers to σ. Late σ-emits enter the Phase-3 σ-pool even if peers'
	// NR/Defer decision has already locked.
	if _, err := s.BuildAndBroadcastOnion(ctx, slot); err != nil {
		return nil, fmt.Errorf("obft scheduler: post-PhaseTwoEnd onion re-emit: %w", err)
	}
	nr, err := s.controller.BuildOwnNR(slot)
	if err != nil {
		return nil, fmt.Errorf("obft scheduler: build own NR: %w", err)
	}
	data, err := wire.WrapNR(nr)
	if err != nil {
		return nil, fmt.Errorf("obft scheduler: wrap NR: %w", err)
	}
	if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
		return nil, fmt.Errorf("obft scheduler: broadcast NR: %w", err)
	}
	return nr, nil
}

// ResolveAndSubmit runs Phase-3 Resolve. On success: hands the Output to
// hooks.SubmitOutput, then (if BroadcastCertificate is set) broadcasts the
// final-certificate gossip message.
//
// On failure: tries falling back to a peer's Certificate (if RetainedCertificate
// returns one); else calls hooks.OnMissedSlot.
func (s *Scheduler) ResolveAndSubmit(ctx context.Context, slot phase0.Slot) error {
	out, err := s.controller.Resolve(slot)
	if err != nil {
		// Local reconstruction failed — try peer-certificate fallback.
		if cert, certErr := s.controller.RetainedCertificate(slot); certErr == nil && cert != nil {
			fallbackOut := &obftcore.Output{
				Value:     []byte(cert.Value),
				Signature: []byte(cert.Signature),
			}
			if subErr := s.hooks.SubmitOutput(ctx, slot, fallbackOut); subErr == nil {
				return nil
			}
		}
		if s.hooks.OnMissedSlot != nil {
			s.hooks.OnMissedSlot(ctx, slot, err)
		}
		return fmt.Errorf("obft scheduler: resolve: %w", err)
	}

	if err := s.hooks.SubmitOutput(ctx, slot, out); err != nil {
		return fmt.Errorf("obft scheduler: submit output: %w", err)
	}

	if s.hooks.BroadcastCertificate != nil {
		cert, err := s.controller.BuildCertificate(slot, out)
		if err != nil {
			return fmt.Errorf("obft scheduler: build certificate: %w", err)
		}
		data, err := wire.WrapCertificate(cert)
		if err != nil {
			return fmt.Errorf("obft scheduler: wrap certificate: %w", err)
		}
		if err := s.hooks.BroadcastCertificate(ctx, slot, data); err != nil {
			return fmt.Errorf("obft scheduler: broadcast certificate: %w", err)
		}
	}
	return nil
}

