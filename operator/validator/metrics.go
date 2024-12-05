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
		recordValidatorStatus(c.ctx, statusNotFound)
	} else if meta.IsActive() {
		logger.Debug("validator is ready")
		recordValidatorStatus(c.ctx, statusReady)
	} else if meta.Slashed() {
		logger.Debug("validator slashed")
		recordValidatorStatus(c.ctx, statusSlashed)
	} else if meta.Exiting() {
		logger.Debug("validator exiting / exited")
		recordValidatorStatus(c.ctx, statusExiting)
	} else if !meta.Activated() {
		logger.Debug("validator not activated")
		recordValidatorStatus(c.ctx, statusNotActivated)
	} else if meta.Pending() {
		logger.Debug("validator pending")
		recordValidatorStatus(c.ctx, statusPending)
	} else if meta.Index == 0 {
		logger.Debug("validator index not found")
		recordValidatorStatus(c.ctx, statusNoIndex)
	} else {
		logger.Debug("validator is unknown")
		recordValidatorStatus(c.ctx, statusUnknown)
	}
}
