package validator

import (
	"encoding/hex"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

// TODO: consider extracting from controller
// TODO: finish, refactor

func (c *controller) AddValidator(validatorAddedEvent *contract.ContractValidatorAdded) error {
	if _, ok := c.validatorsMap.GetValidator(hex.EncodeToString(validatorAddedEvent.PublicKey)); ok {
		return nil
	}

	validatorShare := c.sharesStorage.Get(validatorAddedEvent.PublicKey)
	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if !isOperatorShare {
		return nil
	}

	if _, err := c.onShareStart(c.defaultLogger, validatorShare); err != nil {
		return err
	}

	return nil
}

func (c *controller) RemoveValidator(validatorRemovedEvent *contract.ContractValidatorRemoved) error {
	if _, ok := c.validatorsMap.GetValidator(hex.EncodeToString(validatorRemovedEvent.PublicKey)); ok {
		return nil
	}

	// TODO: it's already removed from storage, consider passing share to RemoveValidator
	validatorShare := c.sharesStorage.Get(validatorRemovedEvent.PublicKey)
	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if !isOperatorShare {
		return nil
	}

	pubKey := hex.EncodeToString(validatorRemovedEvent.PublicKey)
	metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusRemoved))
	if err := c.onShareRemove(hex.EncodeToString(validatorShare.ValidatorPubKey), true); err != nil {
		return err
	}

	return nil
}

func (c *controller) LiquidateCluster(_ *contract.ContractClusterLiquidated, toLiquidate []*ssvtypes.SSVShare) error {
	for _, share := range toLiquidate {
		// we can't remove the share secret from key-manager
		// due to the fact that after activating the validators (ClusterReactivated)
		// we don't have the encrypted keys to decrypt the secret, but only the owner address
		if err := c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), false); err != nil {
			return err
		}
	}

	return nil
}

func (c *controller) ReactivateCluster(_ *contract.ContractClusterReactivated, toEnable []*ssvtypes.SSVShare) error {
	for _, share := range toEnable {
		if _, err := c.onShareStart(c.defaultLogger, share); err != nil {
			return err
		}
	}

	return nil
}

func (c *controller) UpdateFeeRecipient(feeRecipientUpdatedEvent *contract.ContractFeeRecipientAddressUpdated) error {
	// TODO: move to event handler?
	return c.validatorsMap.ForEach(func(v *validator.Validator) error {
		if v.Share.OwnerAddress == feeRecipientUpdatedEvent.Owner {
			v.Share.FeeRecipientAddress = feeRecipientUpdatedEvent.RecipientAddress
		}
		return nil
	})
}
