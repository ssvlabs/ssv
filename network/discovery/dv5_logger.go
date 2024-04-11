package discovery

import (
	"go.uber.org/zap"
)

// dv5Logger implements log.Handler to track logs of discv5
type dv5Logger struct {
	logger *zap.Logger // struct logger to implement log.Handler
}
