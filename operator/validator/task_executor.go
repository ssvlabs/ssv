package validator

import (
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/multierr"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func (c *controller) taskLogger(taskName string, fields ...zap.Field) *zap.Logger {
	return c.logger.Named("TaskExecutor").
		With(zap.String("task", taskName)).
		With(fields...)
}

func (c *controller) StartValidator(share *ssvtypes.SSVShare) error {
	// logger := c.taskLogger("StartValidator", fields.PubKey(share.ValidatorPubKey))

	// Since we don't yet have the Beacon metadata for this validator,
	// we can't yet start it. Starting happens in `UpdateValidatorMetaDataLoop`,
	// so this task is currently a no-op.

	return nil
}

func (c *controller) StopValidator(publicKey []byte) error {
	logger := c.taskLogger("StopValidator", fields.PubKey(publicKey))

	c.metrics.ValidatorRemoved(publicKey)
	if err := c.onShareRemove(hex.EncodeToString(publicKey), true); err != nil {
		return err
	}

	logger.Info("removed validator")

	return nil
}

func (c *controller) LiquidateCluster(owner common.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error {
	logger := c.taskLogger("LiquidateCluster",
		zap.String("owner", owner.String()),
		zap.Uint64s("operator_ids", operatorIDs))

	for _, share := range toLiquidate {
		// we can't remove the share secret from key-manager
		// due to the fact that after activating the validators (ClusterReactivated)
		// we don't have the encrypted keys to decrypt the secret, but only the owner address
		if err := c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), false); err != nil {
			return err
		}
		logger.With(fields.PubKey(share.ValidatorPubKey)).Debug("removed share")
	}

	return nil
}

func (c *controller) ReactivateCluster(owner common.Address, operatorIDs []uint64, toReactivate []*ssvtypes.SSVShare) error {
	logger := c.taskLogger("ReactivateCluster",
		zap.String("owner", owner.String()),
		zap.Uint64s("operator_ids", operatorIDs))

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
		c.indicesChange <- struct{}{}
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

	err := c.validatorsMap.ForEach(func(v *validator.Validator) error {
		if v.Share.OwnerAddress == owner {
			v.Share.FeeRecipientAddress = recipient

			logger.Debug("updated recipient address")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update validators map: %w", err)
	}

	return nil
}
