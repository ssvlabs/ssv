package validation

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
)

// Option defines MessageValidator configuration option.
type Option func(*messageValidator)

// WithLogger sets logger.
func WithLogger(logger *zap.Logger) Option {
	return func(mv *messageValidator) {
		mv.logger = logger
	}
}

// WithSelfAccept sets whether messages from self should be validated.
func WithSelfAccept(selfAccept bool) Option {
	return func(mv *messageValidator) {
		mv.selfAccept = selfAccept
	}
}

// WithSelfPID sets the node's own peer ID.
func WithSelfPID(selfPID peer.ID) Option {
	return func(mv *messageValidator) {
		mv.selfPID = selfPID
	}
}
