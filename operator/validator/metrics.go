package validator

import (
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/types"
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
	} else if share.IsActive() {
		logger.Debug("checking validator: validator is ready")
	} else if share.Slashed() {
		logger.Debug("checking validator: validator slashed")
	} else if share.Exiting() {
		logger.Debug("checking validator: validator exiting / exited")
	} else if !share.Activated() {
		logger.Debug("checking validator: validator not activated")
	} else if share.Pending() {
		logger.Debug("checking validator: validator pending")
	} else if share.ValidatorIndex == 0 {
		logger.Debug("checking validator: validator index not found")
	} else {
		logger.Debug("checking validator: validator is unknown")
	}
}
