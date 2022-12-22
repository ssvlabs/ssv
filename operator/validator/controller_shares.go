package validator

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// TODO: Duplicate
// func (n *operatorNode) init(opts Options) error {
//	if opts.ValidatorOptions.CleanRegistryData {
//		if err := n.storage.CleanRegistryData(); err != nil {
//			return errors.Wrap(err, "failed to clean registry data")
//		}
//	}
//	return nil
//	}
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
