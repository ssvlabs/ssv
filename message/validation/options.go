package validation

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
)

// Option represents a functional option for configuring a messageValidator.
type Option func(validator *messageValidator)

// WithLogger sets the logger for the messageValidator.
func WithLogger(logger *zap.Logger) Option {
	return func(mv *messageValidator) {
		mv.logger = logger
	}
}

// WithSelfAccept blindly accepts messages sent from self. Useful for testing.
func WithSelfAccept(selfPID peer.ID, selfAccept bool) Option {
	return func(mv *messageValidator) {
		mv.selfPID = selfPID
		mv.selfAccept = selfAccept
	}
}
