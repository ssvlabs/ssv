package validator

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// initShares initializes shares, should be called upon creation of controller
func (c *controller) initShares(logger *zap.Logger, options ControllerOptions) error {
	if options.CleanRegistryData {
		if err := c.collection.CleanRegistryData(); err != nil {
			return errors.Wrap(err, "failed to clean validator storage registry data")
		}
		logger.Debug("all shares were removed")
	}

	return nil
}
