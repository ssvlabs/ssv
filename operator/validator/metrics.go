package validator

import (
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"go.uber.org/zap"
)

func (c *controller) reportValidatorStatus(pk []byte, meta *beacon.ValidatorMetadata) {
	logger := c.logger.With(fields.PubKey(pk), zap.Any("metadata", meta))
	if meta == nil {
		logger.Debug("validator metadata not found")
	} else if meta.IsActive() {
		logger.Debug("validator is ready")
	} else if meta.Slashed() {
		logger.Debug("validator slashed")
	} else if meta.Exiting() {
		logger.Debug("validator exiting / exited")
	} else if !meta.Activated() {
		logger.Debug("validator not activated")
	} else if meta.Pending() {
		logger.Debug("validator pending")
	} else if meta.Index == 0 {
		logger.Debug("validator index not found")
	} else {
		logger.Debug("validator is unknown")
	}
}
