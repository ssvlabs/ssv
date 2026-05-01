package tbft

import (
	"context"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/wire"
)

// LifecycleHooks are callbacks the runner provides to the Scheduler. The
// runner controls *when* phases fire; the Scheduler executes the protocol
// mechanics inside each phase and calls the appropriate hook for runner
// concerns (fetching, broadcasting, submitting).
type LifecycleHooks struct {
	// FetchCandidate is invoked when the local operator is the designated
	// leader for `layer` of `slot` and the layer's `FetchAt` time has
	// arrived. The runner fetches a candidate value (e.g. requests a
	// blinded block from the beacon node) and returns its bytes.
	//
	// Required.
	FetchCandidate func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error)

	// Broadcast is invoked to gossip a TBFT wire-envelope-wrapped message
	// (Onion, NonReceipt, or Candidate) to peer operators. The runner
	// places the bytes into a SignedSSVMessage and routes through SSV's
	// existing p2p layer.
	//
	// Required.
	Broadcast func(ctx context.Context, slot phase0.Slot, data []byte) error

	// SubmitOutput is invoked when Resolve produces a signed Output. The
	// runner uses the (Value, Signature) to submit to the beacon network
	// (e.g. publishing the validator-signed block).
	//
	// Required.
	SubmitOutput func(ctx context.Context, slot phase0.Slot, output *tbftcore.Output) error

	// OnMissedSlot is invoked when Resolve returns ErrNoQuorum (or a
	// related failure). Optional — used for telemetry and logging. If
	// nil, missed-slot conditions are silently swallowed at the
	// Scheduler level (they still surface as a non-nil error from
	// ResolveAndSubmit).
	OnMissedSlot func(ctx context.Context, slot phase0.Slot, reason error)
}

func (h *LifecycleHooks) validate() error {
	if h == nil {
		return errors.New("tbft adapter: nil LifecycleHooks")
	}
	if h.FetchCandidate == nil {
		return errors.New("tbft adapter: LifecycleHooks.FetchCandidate is required")
	}
	if h.Broadcast == nil {
		return errors.New("tbft adapter: LifecycleHooks.Broadcast is required")
	}
	if h.SubmitOutput == nil {
		return errors.New("tbft adapter: LifecycleHooks.SubmitOutput is required")
	}
	return nil
}

// Scheduler combines a Controller with LifecycleHooks to expose the per-slot
// phase mechanics as procedural methods. The runner is responsible for
// calling these methods at the right slot offsets — the Scheduler does NOT
// manage time itself. This keeps the Scheduler trivially testable and
// integrates cleanly with SSV's existing slot-ticker infrastructure.
//
// Typical use from a runner:
//
//	r, err := scheduler.Controller().StartNewInstance(slot)
//	... at FetchAt for each layer in r.LeaderAtLayers, on a goroutine:
//	    scheduler.FetchAndBroadcastCandidate(ctx, slot, layer)
//	... at Deadline (T_commit):
//	    scheduler.BuildAndBroadcastOnion(ctx, slot)
//	... at Deadline + GossipWindow:
//	    scheduler.ResolveAndSubmit(ctx, slot)
//	scheduler.Controller().EndInstance(slot)
type Scheduler struct {
	controller *Controller
	hooks      *LifecycleHooks
}

// NewScheduler constructs a Scheduler. The Controller's Signer / TagSigner
// already carry the operator's share — the Scheduler doesn't need its own
// copy. Returns an error if hooks are incomplete.
func NewScheduler(controller *Controller, hooks *LifecycleHooks) (*Scheduler, error) {
	if controller == nil {
		return nil, errors.New("tbft adapter: nil Controller")
	}
	if err := hooks.validate(); err != nil {
		return nil, err
	}
	return &Scheduler{
		controller: controller,
		hooks:      hooks,
	}, nil
}

// Controller returns the underlying Controller. Useful for the runner to
// drive instance lifecycle (StartNewInstance, EndInstance, Process*) and
// inspect RunningInstance metadata (LeaderAtLayers, Config).
func (s *Scheduler) Controller() *Controller {
	return s.controller
}

// FetchAndBroadcastCandidate executes Phase 1 for one layer where the local
// operator is the designated leader. Steps:
//
//  1. Call hooks.FetchCandidate to obtain the candidate value.
//  2. Locally observe the candidate (so the operator's own onion at Phase
//     2 includes this value).
//  3. Wrap as a CandidateBroadcast and broadcast to peers via hooks.Broadcast.
//
// Returns an error if any step fails. The error is propagated unchanged
// from the failing hook.
func (s *Scheduler) FetchAndBroadcastCandidate(ctx context.Context, slot phase0.Slot, layer int) error {
	value, err := s.hooks.FetchCandidate(ctx, slot, layer)
	if err != nil {
		return fmt.Errorf("tbft scheduler: fetch candidate (slot=%d layer=%d): %w", slot, layer, err)
	}

	if err := s.controller.ObserveCandidate(slot, layer, value); err != nil {
		return fmt.Errorf("tbft scheduler: observe own candidate: %w", err)
	}

	cb := &tbftcore.CandidateBroadcast{
		OperatorID: tbftcore.OperatorID(s.controller.OperatorID()),
		Height:     tbftcore.Height(slot),
		Layer:      layer,
		Value:      tbftcore.Value(value),
	}
	data, err := wire.WrapCandidate(cb)
	if err != nil {
		return fmt.Errorf("tbft scheduler: wrap candidate: %w", err)
	}
	if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
		return fmt.Errorf("tbft scheduler: broadcast candidate: %w", err)
	}
	return nil
}

// BuildAndBroadcastOnion executes Phase 2: builds the local operator's
// onion + non-receipt attestations and broadcasts them. Should be called
// at the slot's deadline (T_commit) after Phase-1 candidates have had a chance
// to propagate.
func (s *Scheduler) BuildAndBroadcastOnion(ctx context.Context, slot phase0.Slot) error {
	onion, err := s.controller.BuildOwnOnion(slot)
	if err != nil {
		return fmt.Errorf("tbft scheduler: build own onion: %w", err)
	}
	onionBytes, err := wire.WrapOnion(onion)
	if err != nil {
		return fmt.Errorf("tbft scheduler: wrap onion: %w", err)
	}
	if err := s.hooks.Broadcast(ctx, slot, onionBytes); err != nil {
		return fmt.Errorf("tbft scheduler: broadcast onion: %w", err)
	}

	nrs, err := s.controller.BuildOwnNonReceipts(slot)
	if err != nil {
		return fmt.Errorf("tbft scheduler: build own non-receipts: %w", err)
	}
	for _, nr := range nrs {
		nrBytes, err := wire.WrapNonReceipt(nr)
		if err != nil {
			return fmt.Errorf("tbft scheduler: wrap non-receipt: %w", err)
		}
		if err := s.hooks.Broadcast(ctx, slot, nrBytes); err != nil {
			return fmt.Errorf("tbft scheduler: broadcast non-receipt: %w", err)
		}
	}
	return nil
}

// ResolveAndSubmit executes Phase 3: runs the decryption walk and, on
// success, hands the resulting Output to hooks.SubmitOutput. Should be
// called after the broadcast deadline has elapsed and gossip has had time
// to propagate.
//
// On failure (typically tbft.ErrNoQuorum) calls hooks.OnMissedSlot if set
// and returns the error. The runner is expected to invoke
// `Scheduler.Controller().EndInstance(slot)` afterwards regardless of
// outcome.
func (s *Scheduler) ResolveAndSubmit(ctx context.Context, slot phase0.Slot) error {
	output, err := s.controller.Resolve(slot)
	if err != nil {
		if s.hooks.OnMissedSlot != nil {
			s.hooks.OnMissedSlot(ctx, slot, err)
		}
		return fmt.Errorf("tbft scheduler: resolve: %w", err)
	}
	if err := s.hooks.SubmitOutput(ctx, slot, output); err != nil {
		return fmt.Errorf("tbft scheduler: submit output: %w", err)
	}
	return nil
}
