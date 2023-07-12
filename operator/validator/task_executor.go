package validator

import (
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func (c *controller) AddValidator(publicKey []byte) error {
	logger := c.defaultLogger.Named("AddValidator").
		With(fields.PubKey(publicKey))

	logger.Info("executing task")

	if _, ok := c.validatorsMap.GetValidator(hex.EncodeToString(publicKey)); ok {
		logger.Debug("validator has already started")
		return nil
	}

	validatorShare := c.sharesStorage.Get(publicKey)
	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if !isOperatorShare {
		logger.Debug("not operator share")
		return nil
	}

	logger.Debug("going to start validator")
	if _, err := c.onShareStart(c.defaultLogger, validatorShare); err != nil {
		return err
	}

	logger.Info("started validator")
	return nil
}

func (c *controller) RemoveValidator(publicKey []byte) error {
	logger := c.defaultLogger.Named("RemoveValidator").
		With(fields.PubKey(publicKey))

	logger.Info("executing task")

	if _, ok := c.validatorsMap.GetValidator(hex.EncodeToString(publicKey)); ok {
		logger.Debug("validator has not started")
		return nil
	}

	// TODO: it's already removed from storage, consider passing share to RemoveValidator
	validatorShare := c.sharesStorage.Get(publicKey)
	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if !isOperatorShare {
		logger.Debug("not operator share")
		return nil
	}

	c.metrics.ValidatorRemoved(publicKey)
	if err := c.onShareRemove(hex.EncodeToString(validatorShare.ValidatorPubKey), true); err != nil {
		return err
	}

	logger.Info("removed validator")

	return nil
}

func (c *controller) LiquidateCluster(owner common.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error {
	logger := c.defaultLogger.Named("LiquidateCluster").With(
		zap.String("owner", owner.String()),
		zap.Uint64s("operator_ids", operatorIDs),
	)
	logger.Info("executing task")

	for _, share := range toLiquidate {
		// we can't remove the share secret from key-manager
		// due to the fact that after activating the validators (ClusterReactivated)
		// we don't have the encrypted keys to decrypt the secret, but only the owner address
		if err := c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), false); err != nil {
			return err
		}
		logger.With(fields.PubKey(share.ValidatorPubKey)).Debug("removed share")
	}

	logger.Info("executed task")
	return nil
}

func (c *controller) ReactivateCluster(owner common.Address, operatorIDs []uint64, toEnable []*ssvtypes.SSVShare) error {
	logger := c.defaultLogger.Named("ReactivateCluster").With(
		zap.String("owner", owner.String()),
		zap.Uint64s("operator_ids", operatorIDs),
	)
	logger.Info("executing task")

	for _, share := range toEnable {
		if _, err := c.onShareStart(c.defaultLogger, share); err != nil {
			return err
		}
		logger.Info("started share")
	}

	logger.Info("executed task")
	return nil
}

func (c *controller) UpdateFeeRecipient(owner, recipient common.Address) error {
	logger := c.defaultLogger.Named("UpdateFeeRecipient").With(
		zap.String("owner", owner.String()),
		zap.String("fee_recipient", recipient.String()),
	)
	logger.Info("executing task")

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

	logger.Info("executed task")
	return nil
}
