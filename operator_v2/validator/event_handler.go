package validator

import (
	"encoding/hex"
	"strings"

	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/exporter"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	registrystorage "github.com/bloxapp/ssv/registry/storage"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Eth1EventHandler is a factory function for creating eth1 event handler
func (c *controller) Eth1EventHandler(ongoingSync bool) eth1.SyncEventHandler {
	return func(e eth1.Event) ([]zap.Field, error) {
		switch e.Name {
		case abiparser.OperatorRegistration:
			ev := e.Data.(abiparser.OperatorRegistrationEvent)
			return c.handleOperatorRegistrationEvent(ev)
		case abiparser.OperatorRemoval:
			ev := e.Data.(abiparser.OperatorRemovalEvent)
			return c.handleOperatorRemovalEvent(ev, ongoingSync)
		case abiparser.ValidatorRegistration:
			ev := e.Data.(abiparser.ValidatorRegistrationEvent)
			return c.handleValidatorRegistrationEvent(ev, ongoingSync)
		case abiparser.ValidatorRemoval:
			ev := e.Data.(abiparser.ValidatorRemovalEvent)
			return c.handleValidatorRemovalEvent(ev, ongoingSync)
		case abiparser.AccountLiquidation:
			ev := e.Data.(abiparser.AccountLiquidationEvent)
			return c.handleAccountLiquidationEvent(ev, ongoingSync)
		case abiparser.AccountEnable:
			ev := e.Data.(abiparser.AccountEnableEvent)
			return c.handleAccountEnableEvent(ev, ongoingSync)
		default:
			c.logger.Debug("could not handle unknown event")
		}
		return nil, nil
	}
}

// handleOperatorRegistrationEvent parses the given event and saves operator data
func (c *controller) handleOperatorRegistrationEvent(event abiparser.OperatorRegistrationEvent) ([]zap.Field, error) {
	eventOperatorPubKey := string(event.PublicKey)
	od := registrystorage.OperatorData{
		PublicKey:    eventOperatorPubKey,
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
		Index:        uint64(event.Id),
	}
	if err := c.storage.SaveOperatorData(&od); err != nil {
		return nil, errors.Wrap(err, "could not save operator data")
	}

	logFields := make([]zap.Field, 0)
	if strings.EqualFold(eventOperatorPubKey, c.operatorPubKey) || c.validatorOptions.Mode == validator.ModeRW {
		logFields = append(logFields,
			zap.String("operatorName", od.Name),
			zap.Uint64("operatorId", od.Index),
			zap.String("operatorPubKey", od.PublicKey),
			zap.String("ownerAddress", od.OwnerAddress.String()),
		)
	}
	exporter.ReportOperatorIndex(c.logger, &od)
	return logFields, nil
}

// handleOperatorRemovalEvent parses the given event and removing operator data
func (c *controller) handleOperatorRemovalEvent(
	event abiparser.OperatorRemovalEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	od, found, err := c.storage.GetOperatorData(uint64(event.OperatorId))
	if err != nil {
		return nil, errors.Wrap(err, "could not get operator data")
	}
	if !found {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find operator data"),
		}
	}

	// this check is deprecated, since the validation is happening on the contract side
	//if od.OwnerAddress != event.OwnerAddress {
	//	return nil, &abiparser.MalformedEventError{
	//		Err: errors.New("could not match operator owner address with provided event owner address"),
	//	}
	//}

	// TODO: check by operator ID, not operator public key
	isOperatorEvent := strings.EqualFold(od.PublicKey, c.operatorPubKey)
	logFields := make([]zap.Field, 0)
	if isOperatorEvent || c.validatorOptions.Mode == validator.ModeRW {
		logFields = append(logFields,
			zap.String("operatorName", od.Name),
			zap.Uint64("operatorId", od.Index),
			zap.String("operatorPubKey", od.PublicKey),
			zap.String("ownerAddress", od.OwnerAddress.String()),
		)
	}

	if !isOperatorEvent {
		// TODO: remove this check when we will support operator removal for non-operator (mark as inactive)
		return logFields, nil
	}

	shareList, _, err := c.collection.GetOperatorIDValidatorShares(event.OperatorId, false)
	if err != nil {
		return nil, errors.Wrap(err, "could not get all operator validator shares")
	}

	for _, share := range shareList {
		if err := c.collection.DeleteValidatorShare(share.ValidatorPubKey); err != nil {
			return nil, errors.Wrap(err, "could not remove validator share")
		}
		if err := c.collection.DeleteShareMetadata(share.ValidatorPubKey); err != nil {
			return nil, errors.Wrap(err, "could not remove share metadata")
		}
		if ongoingSync {
			if err := c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), true); err != nil {
				return nil, err
			}
		}
	}

	err = c.storage.DeleteOperatorData(uint64(event.OperatorId))
	if err != nil {
		return nil, errors.Wrap(err, "could not delete operator data")
	}

	return logFields, nil
}

// handleValidatorRegistrationEvent handles registry contract event for validator added
func (c *controller) handleValidatorRegistrationEvent(
	validatorRegistrationEvent abiparser.ValidatorRegistrationEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	pubKey := hex.EncodeToString(validatorRegistrationEvent.PublicKey)
	if ongoingSync {
		if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
			c.logger.Debug("validator was loaded already")
			return nil, nil
		}
	}

	metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
	validatorMetadata, found, err := c.collection.GetShareMetadata(validatorRegistrationEvent.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	var validatorShare *spectypes.Share
	if !found {
		validatorShare, validatorMetadata, _, err = c.onShareCreate(validatorRegistrationEvent)
		if err != nil {
			metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusError))
			return nil, err
		}
	}

	logFields := make([]zap.Field, 0)
	isOperatorShare := validatorMetadata.BelongsToOperator(c.operatorPubKey)
	if isOperatorShare {
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
		if ongoingSync {
			c.onShareStart(validatorShare, validatorMetadata)
		}
	}

	if isOperatorShare || c.validatorOptions.Mode == validator.ModeRW {
		logFields = append(logFields,
			zap.String("validatorPubKey", pubKey),
			zap.String("ownerAddress", validatorMetadata.OwnerAddress),
			zap.Uint32s("operatorIds", validatorRegistrationEvent.OperatorIds),
		)
	}

	return logFields, nil
}

// handleValidatorRemovalEvent handles registry contract event for validator removed
func (c *controller) handleValidatorRemovalEvent(
	validatorRemovalEvent abiparser.ValidatorRemovalEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	// TODO: handle metrics
	validatorMetadata, found, err := c.collection.GetShareMetadata(validatorRemovalEvent.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find validator share"),
		}
	}

	// this check is deprecated, since the validation is happening on the contract side
	//if validatorShare.OwnerAddress != validatorRemovalEvent.OwnerAddress.String() {
	//	return nil, &abiparser.MalformedEventError{
	//		Err: errors.New("could not match validator owner address with provided event owner address"),
	//	}
	//}

	// remove decided messages
	messageID := spectypes.NewMsgID(validatorMetadata.PublicKey.Serialize(), spectypes.BNRoleAttester)
	if err := c.ibftStorage.CleanAllDecided(messageID[:]); err != nil { // TODO need to delete for multi duty as well
		return nil, errors.Wrap(err, "could not clean all decided messages")
	}
	// remove change round messages
	if err := c.ibftStorage.CleanLastChangeRound(messageID[:]); err != nil { // TODO need to delete for multi duty as well
		return nil, errors.Wrap(err, "could not clean last change round")
	}
	// remove from storage
	if err := c.collection.DeleteValidatorShare(validatorMetadata.PublicKey.Serialize()); err != nil {
		return nil, errors.Wrap(err, "could not remove validator share")
	}

	if err := c.collection.DeleteShareMetadata(validatorMetadata.PublicKey.Serialize()); err != nil {
		return nil, errors.Wrap(err, "could not remove share metadata")
	}

	logFields := make([]zap.Field, 0)
	isOperatorShare := validatorMetadata.BelongsToOperator(c.operatorPubKey)
	if isOperatorShare {
		if ongoingSync {
			if err := c.onShareRemove(validatorMetadata.PublicKey.SerializeToHexStr(), true); err != nil {
				return nil, err
			}
		}
	}

	if isOperatorShare || c.validatorOptions.Mode == validator.ModeRW {
		logFields = append(logFields,
			zap.String("validatorPubKey", validatorMetadata.PublicKey.SerializeToHexStr()),
			zap.String("ownerAddress", validatorMetadata.OwnerAddress),
		)
	}

	return logFields, nil
}

// handleAccountLiquidationEvent handles registry contract event for account liquidated
func (c *controller) handleAccountLiquidationEvent(
	event abiparser.AccountLiquidationEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	ownerAddress := event.OwnerAddress.String()
	_, metadataList, err := c.collection.GetValidatorMetadataByOwnerAddress(ownerAddress)
	if err != nil {
		return nil, errors.Wrap(err, "could not get validator shares by owner address")
	}
	operatorSharePubKeys := make([]string, 0)

	for _, metadata := range metadataList {
		isOperatorShare := metadata.BelongsToOperator(c.operatorPubKey)
		if isOperatorShare || c.validatorOptions.Mode == validator.ModeRW {
			operatorSharePubKeys = append(operatorSharePubKeys, metadata.PublicKey.SerializeToHexStr())
		}
		if isOperatorShare {
			metadata.Liquidated = true

			// save validator data
			if err := c.collection.SaveShareMetadata(metadata); err != nil {
				return nil, errors.Wrap(err, "could not save validator share")
			}

			if ongoingSync {
				// we can't remove the share secret from key-manager
				// due to the fact that after activating the validators (AccountEnable)
				// we don't have the encrypted keys to decrypt the secret, but only the owner address
				if err := c.onShareRemove(metadata.PublicKey.SerializeToHexStr(), false); err != nil {
					return nil, err
				}
			}
		}
	}

	logFields := make([]zap.Field, 0)
	if len(operatorSharePubKeys) > 0 {
		logFields = append(logFields,
			zap.String("ownerAddress", event.OwnerAddress.String()),
			zap.Strings("liquidatedShares", operatorSharePubKeys),
		)
	}

	return logFields, nil
}

// handle AccountEnableEvent handles registry contract event for account enabled
func (c *controller) handleAccountEnableEvent(
	event abiparser.AccountEnableEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	ownerAddress := event.OwnerAddress.String()
	shareList, metadataList, err := c.collection.GetValidatorMetadataByOwnerAddress(ownerAddress)
	if err != nil {
		return nil, errors.Wrap(err, "could not get validator shares by owner address")
	}
	operatorSharePubKeys := make([]string, 0)

	for i, metadata := range metadataList {
		isOperatorShare := metadata.BelongsToOperator(c.operatorPubKey)
		if isOperatorShare || c.validatorOptions.Mode == validator.ModeRW {
			operatorSharePubKeys = append(operatorSharePubKeys, metadata.PublicKey.SerializeToHexStr())
		}
		if metadata.BelongsToOperator(c.operatorPubKey) {
			metadata.Liquidated = false

			// save validator data
			if err := c.collection.SaveShareMetadata(metadata); err != nil {
				return nil, errors.Wrap(err, "could not save validator metadata")
			}

			if ongoingSync {
				c.onShareStart(shareList[i], metadata)
			}
		}
	}

	logFields := make([]zap.Field, 0)
	if len(operatorSharePubKeys) > 0 {
		logFields = append(logFields,
			zap.String("ownerAddress", event.OwnerAddress.String()),
			zap.Strings("enabledShares", operatorSharePubKeys),
		)
	}

	return logFields, nil
}
