package validator

import (
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/multierr"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

func (c *controller) taskLogger(taskName string, fields ...zap.Field) *zap.Logger {
	return c.logger.Named("TaskExecutor").
		With(zap.String("task", taskName)).
		With(fields...)
}

func (c *controller) StartValidator(share *types.SSVShare) error {
	// logger := c.taskLogger("StartValidator", fields.PubKey(share.ValidatorPubKey))

	// Since we don't yet have the Beacon metadata for this validator,
	// we can't yet start it. Starting happens in `UpdateValidatorMetaDataLoop`,
	// so this task is currently a no-op.

	return nil
}

func (c *controller) StopValidator(pubKey spectypes.ValidatorPK) error {
	logger := c.taskLogger("StopValidator", fields.PubKey(pubKey))

	c.metrics.ValidatorRemoved(pubKey)
	c.onShareStop(pubKey)

	logger.Info("removed validator")

	return nil
}

func (c *controller) LiquidateCluster(owner common.Address, operatorIDs []spectypes.OperatorID, toLiquidate []*types.SSVShare) error {
	logger := c.taskLogger("LiquidateCluster", fields.Owner(owner), fields.OperatorIDs(operatorIDs))

	for _, share := range toLiquidate {
		c.onShareStop(share.ValidatorPubKey)
		logger.With(fields.PubKey(share.ValidatorPubKey)).Debug("liquidated share")
	}

	return nil
}

func (c *controller) ReactivateCluster(owner common.Address, operatorIDs []spectypes.OperatorID, toReactivate []*types.SSVShare) error {
	logger := c.taskLogger("ReactivateCluster", fields.Owner(owner), fields.OperatorIDs(operatorIDs))

	var startedValidators int
	var errs error
	for _, share := range toReactivate {
		started, err := c.onShareStart(share)
		if err != nil {
			errs = multierr.Append(errs, err)
			continue
		}
		if started {
			startedValidators++
		}
	}
	if startedValidators > 0 {
		// Notify DutyScheduler about the changes in validator indices without blocking.
		go func() {
			select {
			case c.indicesChange <- struct{}{}:
			case <-time.After(12 * time.Second):
				logger.Error("failed to notify indices change")
			}
		}()
	}
	logger.Debug("reactivated cluster",
		zap.Int("cluster_validators", len(toReactivate)),
		zap.Int("started_validators", startedValidators))

	return errs
}

func (c *controller) UpdateFeeRecipient(owner, recipient common.Address) error {
	logger := c.taskLogger("UpdateFeeRecipient",
		zap.String("owner", owner.String()),
		zap.String("fee_recipient", recipient.String()))

	c.validatorsMap.ForEach(func(v *validator.Validator) bool {
		if v.Share.OwnerAddress == owner {
			v.Share.FeeRecipientAddress = recipient

			logger.Debug("updated recipient address")
		}
		return true
	})

	return nil
}
