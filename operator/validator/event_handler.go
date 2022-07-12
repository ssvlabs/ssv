package validator

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/exporter"
	"strings"

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
	if strings.EqualFold(eventOperatorPubKey, c.operatorPubKey) {
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

	if od.OwnerAddress != event.OwnerAddress {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not match operator data owner address and index with provided event"),
		}
	}

	shares, err := c.collection.GetOperatorValidatorShares(od.PublicKey, false)
	if err != nil {
		return nil, errors.Wrap(err, "could not get all operator validator shares")
	}

	for _, share := range shares {
		if err := c.collection.DeleteValidatorShare(share.PublicKey.Serialize()); err != nil {
			return nil, errors.Wrap(err, "could not remove validator share")
		}
		if ongoingSync {
			if err := c.onShareRemove(share.PublicKey.SerializeToHexStr(), true); err != nil {
				return nil, err
			}
		}
	}

	err = c.storage.DeleteOperatorData(uint64(event.OperatorId))
	if err != nil {
		return nil, errors.Wrap(err, "could not delete operator data")
	}

	logFields := make([]zap.Field, 0)
	if strings.EqualFold(od.PublicKey, c.operatorPubKey) {
		logFields = append(logFields,
			zap.String("operatorName", od.Name),
			zap.Uint64("operatorId", od.Index),
			zap.String("operatorPubKey", od.PublicKey),
			zap.String("ownerAddress", od.OwnerAddress.String()),
		)
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
	validatorShare, found, err := c.collection.GetValidatorShare(validatorRegistrationEvent.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		validatorShare, _, err = c.onShareCreate(validatorRegistrationEvent)
		if err != nil {
			metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusError))
			return nil, err
		}
	}

	logFields := make([]zap.Field, 0)
	isOperatorShare := validatorShare.IsOperatorShare(c.operatorPubKey)
	if isOperatorShare {
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
		if ongoingSync {
			c.onShareStart(validatorShare)
		}
		logFields = append(logFields,
			zap.String("validatorPubKey", pubKey),
			zap.String("ownerAddress", validatorShare.OwnerAddress),
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
	validatorShare, found, err := c.collection.GetValidatorShare(validatorRemovalEvent.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find validator share"),
		}
	}

	// remove from storage
	if err := c.collection.DeleteValidatorShare(validatorShare.PublicKey.Serialize()); err != nil {
		return nil, errors.Wrap(err, "could not remove validator share")
	}

	logFields := make([]zap.Field, 0)
	isOperatorShare := validatorShare.IsOperatorShare(c.operatorPubKey)
	if isOperatorShare {
		if ongoingSync {
			if err := c.onShareRemove(validatorShare.PublicKey.SerializeToHexStr(), true); err != nil {
				return nil, err
			}
		}
		logFields = append(logFields,
			zap.String("validatorPubKey", validatorShare.PublicKey.SerializeToHexStr()),
			zap.String("ownerAddress", validatorShare.OwnerAddress),
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
	shares, err := c.collection.GetValidatorSharesByOwnerAddress(ownerAddress)
	if err != nil {
		return nil, errors.Wrap(err, "could not get validator shares by owner address")
	}
	operatorSharePubKeys := make([]string, 0)

	for _, share := range shares {
		if share.IsOperatorShare(c.operatorPubKey) {
			operatorSharePubKeys = append(operatorSharePubKeys, share.PublicKey.SerializeToHexStr())
			share.Liquidated = true

			// save validator data
			if err := c.collection.SaveValidatorShare(share); err != nil {
				return nil, errors.Wrap(err, "could not save validator share")
			}

			if ongoingSync {
				// we can't remove the share secret from key-manager
				// due to the fact that after activating the validators (AccountEnable)
				// we don't have the encrypted keys to decrypt the secret, but only the owner address
				if err := c.onShareRemove(share.PublicKey.SerializeToHexStr(), false); err != nil {
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
	shares, err := c.collection.GetValidatorSharesByOwnerAddress(ownerAddress)
	if err != nil {
		return nil, errors.Wrap(err, "could not get validator shares by owner address")
	}
	operatorSharePubKeys := make([]string, 0)

	for _, share := range shares {
		if share.IsOperatorShare(c.operatorPubKey) {
			operatorSharePubKeys = append(operatorSharePubKeys, share.PublicKey.SerializeToHexStr())
			share.Liquidated = false

			// save validator data
			if err := c.collection.SaveValidatorShare(share); err != nil {
				return nil, errors.Wrap(err, "could not save validator share")
			}

			if ongoingSync {
				c.onShareStart(share)
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
