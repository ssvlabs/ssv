package validator

import (
	"github.com/pkg/errors"
)

// initShares initializes shares, should be called upon creation of controller
func (c *controller) initShares(options ControllerOptions) error {
	if options.CleanRegistryData {
		if err := c.collection.CleanRegistryData(); err != nil {
			return errors.Wrap(err, "failed to clean validator storage registry data")
		}
		c.logger.Debug("all shares were removed")
	}

	return nil
}
