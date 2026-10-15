package validation

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Option represents a functional option for configuring a messageValidator.
type Option func(validator *messageValidator)

// WithLogger sets the logger for the messageValidator.
func WithLogger(logger *zap.Logger) Option {
	return func(mv *messageValidator) {
		mv.logger = logger
	}
}

// AttributionObserver receives (authoring peer, signer operator) pairs for accepted
// single-signer messages. Exporters use it to attribute peers to operators.
type AttributionObserver interface {
	Observe(p peer.ID, op spectypes.OperatorID)
}

// WithAttribution makes the validator report accepted single-signer messages to o.
func WithAttribution(o AttributionObserver) Option {
	return func(mv *messageValidator) {
		mv.attribution = o
	}
}
