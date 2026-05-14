package dkg

import (
	"errors"
	"fmt"

	"github.com/drand/kyber/share/dkg"
	"go.uber.org/zap"

	dkgwire "github.com/ssvlabs/ssv/protocol/v2/dkg/wire"
)

// kyberBoard implements kyber's `dkg.Board` over an SSV-side Transport.
// One kyberBoard instance per active DKG ceremony.
//
// Outbound flow: the kyber DKG protocol calls Push* with a kyber bundle;
// kyberBoard wraps it in the appropriate wire envelope (with our
// clusterID + generation routing fields) and broadcasts it via the
// Transport. The kyber bundle is opaque from the Transport's perspective.
//
// Inbound flow: the Coordinator's inbox-pumping goroutine receives raw
// envelope bytes from the Transport, decodes them, filters by
// (clusterID, generation), and calls Receive on this Board. Receive
// routes the bundle to the matching kyber channel (dealCh / responseCh /
// justificationCh) where the kyber DKG protocol picks it up via its
// IncomingDeal / IncomingResponse / IncomingJustification entry points.
type kyberBoard struct {
	log             *zap.Logger
	broadcast       func([]byte) error
	clusterID       [32]byte
	generation      uint64
	dealCh          chan dkg.DealBundle
	responseCh      chan dkg.ResponseBundle
	justificationCh chan dkg.JustificationBundle
}

// boardChannelBuffer sizes the per-kind incoming channels. A small buffer
// is sufficient because the kyber DKG protocol drains each channel
// promptly; sizing for n peers per kind is a generous upper bound.
const boardChannelBuffer = 32

func newKyberBoard(log *zap.Logger, broadcast func([]byte) error, clusterID [32]byte, generation uint64) *kyberBoard {
	if log == nil {
		log = zap.NewNop()
	}
	return &kyberBoard{
		log:             log,
		broadcast:       broadcast,
		clusterID:       clusterID,
		generation:      generation,
		dealCh:          make(chan dkg.DealBundle, boardChannelBuffer),
		responseCh:      make(chan dkg.ResponseBundle, boardChannelBuffer),
		justificationCh: make(chan dkg.JustificationBundle, boardChannelBuffer),
	}
}

// PushDeals encodes the kyber DealBundle in a wire DealEnvelope with this
// ceremony's clusterID/generation and broadcasts the resulting bytes.
// Errors are logged rather than surfaced — kyber's Board interface has
// no return value here, and a transient broadcast failure should not
// abort the protocol (peers may still hear it via gossip retries).
func (b *kyberBoard) PushDeals(bundle *dkg.DealBundle) {
	bytes, err := dkgwire.WrapDeal(&dkgwire.DealEnvelope{
		ClusterID:  b.clusterID,
		Generation: b.generation,
		Bundle:     bundle,
	})
	if err != nil {
		b.log.Error("dkg-board: encode deal envelope", zap.Error(err))
		return
	}
	if err := b.broadcast(bytes); err != nil {
		b.log.Error("dkg-board: broadcast deal", zap.Error(err))
	}
}

// IncomingDeal returns the channel kyber consumes deal bundles from.
func (b *kyberBoard) IncomingDeal() <-chan dkg.DealBundle {
	return b.dealCh
}

// PushResponses encodes the kyber ResponseBundle and broadcasts it.
func (b *kyberBoard) PushResponses(bundle *dkg.ResponseBundle) {
	bytes, err := dkgwire.WrapResponse(&dkgwire.ResponseEnvelope{
		ClusterID:  b.clusterID,
		Generation: b.generation,
		Bundle:     bundle,
	})
	if err != nil {
		b.log.Error("dkg-board: encode response envelope", zap.Error(err))
		return
	}
	if err := b.broadcast(bytes); err != nil {
		b.log.Error("dkg-board: broadcast response", zap.Error(err))
	}
}

// IncomingResponse returns the channel kyber consumes response bundles from.
func (b *kyberBoard) IncomingResponse() <-chan dkg.ResponseBundle {
	return b.responseCh
}

// PushJustifications encodes the kyber JustificationBundle and broadcasts it.
func (b *kyberBoard) PushJustifications(bundle *dkg.JustificationBundle) {
	bytes, err := dkgwire.WrapJustification(&dkgwire.JustificationEnvelope{
		ClusterID:  b.clusterID,
		Generation: b.generation,
		Bundle:     bundle,
	})
	if err != nil {
		b.log.Error("dkg-board: encode justification envelope", zap.Error(err))
		return
	}
	if err := b.broadcast(bytes); err != nil {
		b.log.Error("dkg-board: broadcast justification", zap.Error(err))
	}
}

// IncomingJustification returns the channel kyber consumes justification
// bundles from.
func (b *kyberBoard) IncomingJustification() <-chan dkg.JustificationBundle {
	return b.justificationCh
}

// Receive routes a wire-decoded inbound envelope to the appropriate kyber
// channel, after filtering by (clusterID, generation). Returns an error
// only on protocol violations (wrong cluster/generation, wrong kind, nil
// envelope). Channel-full conditions are surfaced as errors so the
// caller can decide whether to drop or backoff.
func (b *kyberBoard) Receive(env *dkgwire.Envelope) error {
	if env == nil {
		return errors.New("dkg-board: nil envelope")
	}
	switch env.Kind {
	case dkgwire.KindDeal:
		if env.Deal == nil {
			return errors.New("dkg-board: deal envelope missing body")
		}
		if env.Deal.ClusterID != b.clusterID || env.Deal.Generation != b.generation {
			return fmt.Errorf("dkg-board: deal for foreign cluster/generation (%x/%d)", env.Deal.ClusterID[:8], env.Deal.Generation)
		}
		return enqueue(b.dealCh, *env.Deal.Bundle)
	case dkgwire.KindResponse:
		if env.Response == nil {
			return errors.New("dkg-board: response envelope missing body")
		}
		if env.Response.ClusterID != b.clusterID || env.Response.Generation != b.generation {
			return fmt.Errorf("dkg-board: response for foreign cluster/generation")
		}
		return enqueue(b.responseCh, *env.Response.Bundle)
	case dkgwire.KindJustification:
		if env.Justification == nil {
			return errors.New("dkg-board: justification envelope missing body")
		}
		if env.Justification.ClusterID != b.clusterID || env.Justification.Generation != b.generation {
			return fmt.Errorf("dkg-board: justification for foreign cluster/generation")
		}
		return enqueue(b.justificationCh, *env.Justification.Bundle)
	default:
		return fmt.Errorf("dkg-board: unexpected envelope kind 0x%02x", byte(env.Kind))
	}
}

func enqueue[T any](ch chan T, v T) error {
	select {
	case ch <- v:
		return nil
	default:
		return errors.New("dkg-board: incoming channel full")
	}
}
