package validator

import (
	"encoding/hex"
	"fmt"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func (c *controller) AddValidator(validatorAddedEvent *contract.ContractValidatorAdded) error {
	logger := c.defaultLogger.Named("AddValidator").
		With(fields.PubKey(validatorAddedEvent.PublicKey))

	logger.Debug("executing task")

	if _, ok := c.validatorsMap.GetValidator(hex.EncodeToString(validatorAddedEvent.PublicKey)); ok {
		logger.Debug("validator has already started")
		return nil
	}

	validatorShare := c.sharesStorage.Get(validatorAddedEvent.PublicKey)
	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if !isOperatorShare {
		logger.Debug("not operator share")
		return nil
	}

	logger.Debug("going to start validator")
	if _, err := c.onShareStart(c.defaultLogger, validatorShare); err != nil {
		return err
	}

	logger.Info("started share")
	return nil
}

func (c *controller) RemoveValidator(validatorRemovedEvent *contract.ContractValidatorRemoved) error {
	logger := c.defaultLogger.Named("RemoveValidator").
		With(fields.PubKey(validatorRemovedEvent.PublicKey))

	logger.Debug("executing task")

	if _, ok := c.validatorsMap.GetValidator(hex.EncodeToString(validatorRemovedEvent.PublicKey)); ok {
		logger.Debug("validator has not started")
		return nil
	}

	// TODO: it's already removed from storage, consider passing share to RemoveValidator
	validatorShare := c.sharesStorage.Get(validatorRemovedEvent.PublicKey)
	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if !isOperatorShare {
		logger.Debug("not operator share")
		return nil
	}

	pubKey := hex.EncodeToString(validatorRemovedEvent.PublicKey)
	metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusRemoved))
	if err := c.onShareRemove(hex.EncodeToString(validatorShare.ValidatorPubKey), true); err != nil {
		return err
	}

	logger.Info("removed share")

	return nil
}

func (c *controller) LiquidateCluster(liquidateClusterEvent *contract.ContractClusterLiquidated, toLiquidate []*ssvtypes.SSVShare) error {
	logger := c.defaultLogger.Named("LiquidateCluster").With(
		zap.String("owner", liquidateClusterEvent.Owner.String()),
		zap.Uint64s("operator_ids", liquidateClusterEvent.OperatorIds),
	)
	logger.Debug("executing task")

	for _, share := range toLiquidate {
		// we can't remove the share secret from key-manager
		// due to the fact that after activating the validators (ClusterReactivated)
		// we don't have the encrypted keys to decrypt the secret, but only the owner address
		if err := c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), false); err != nil {
			return err
		}
		logger.With(fields.PubKey(share.ValidatorPubKey)).Debug("removed share")
	}

	logger.Debug("executed task")
	return nil
}

func (c *controller) ReactivateCluster(reactivateClusterEvent *contract.ContractClusterReactivated, toEnable []*ssvtypes.SSVShare) error {
	logger := c.defaultLogger.Named("ReactivateCluster").With(
		zap.String("owner", reactivateClusterEvent.Owner.String()),
		zap.Uint64s("operator_ids", reactivateClusterEvent.OperatorIds),
	)
	logger.Debug("executing task")

	for _, share := range toEnable {
		if _, err := c.onShareStart(c.defaultLogger, share); err != nil {
			return err
		}
		logger.Info("started share")
	}

	logger.Debug("executed task")
	return nil
}

func (c *controller) UpdateFeeRecipient(feeRecipientUpdatedEvent *contract.ContractFeeRecipientAddressUpdated) error {
	logger := c.defaultLogger.Named("UpdateFeeRecipient").With(
		zap.String("owner", feeRecipientUpdatedEvent.Owner.String()),
		zap.String("fee_recipient", feeRecipientUpdatedEvent.RecipientAddress.String()),
	)
	logger.Debug("executing task")

	// TODO: move to event handler?
	err := c.validatorsMap.ForEach(func(v *validator.Validator) error {
		if v.Share.OwnerAddress == feeRecipientUpdatedEvent.Owner {
			v.Share.FeeRecipientAddress = feeRecipientUpdatedEvent.RecipientAddress

			logger.Debug("updated recipient address")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update validators map: %w", err)
	}

	logger.Debug("executed task")
	return nil
}
