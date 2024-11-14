package validator

import (
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

func (c *controller) reportValidatorStatus(share *types.SSVShare) {
	if share == nil {
		c.logger.Debug("checking validator: validator share not found")
		return
	}

	pk := share.ValidatorPubKey[:]
	logger := c.logger.With(fields.PubKey(pk), zap.Any("share", share))

	if !share.HasBeaconMetadata() {
		c.logger.Debug("checking validator: validator has no beacon metadata")
		c.metrics.ValidatorNotFound(pk)
	} else if share.IsActive() {
		logger.Debug("checking validator: validator is ready")
		c.metrics.ValidatorReady(pk)
	} else if share.Slashed() {
		logger.Debug("checking validator: validator slashed")
		c.metrics.ValidatorSlashed(pk)
	} else if share.Exiting() {
		logger.Debug("checking validator: validator exiting / exited")
		c.metrics.ValidatorExiting(pk)
	} else if !share.Activated() {
		logger.Debug("checking validator: validator not activated")
		c.metrics.ValidatorNotActivated(pk)
	} else if share.Pending() {
		logger.Debug("checking validator: validator pending")
		c.metrics.ValidatorPending(pk)
	} else if share.ValidatorIndex == 0 {
		logger.Debug("checking validator: validator index not found")
		c.metrics.ValidatorNoIndex(pk)
	} else {
		logger.Debug("checking validator: validator is unknown")
		c.metrics.ValidatorUnknown(pk)
	}
}
