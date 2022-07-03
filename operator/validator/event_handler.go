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
		case abiparser.OperatorAdded:
			ev := e.Data.(abiparser.OperatorAddedEvent)
			return c.handleOperatorAddedEvent(ev)
		case abiparser.OperatorRemoved:
			// TODO: should we stop all validators owned by this deleted operator?
			ev := e.Data.(abiparser.OperatorRemovedEvent)
			return c.handleOperatorRemovedEvent(ev, ongoingSync)
		case abiparser.ValidatorAdded:
			ev := e.Data.(abiparser.ValidatorAddedEvent)
			return c.handleValidatorAddedEvent(ev, ongoingSync)
		case abiparser.ValidatorRemoved:
			ev := e.Data.(abiparser.ValidatorRemovedEvent)
			return c.handleValidatorRemovedEvent(ev, ongoingSync)
		case abiparser.AccountLiquidated:
			ev := e.Data.(abiparser.AccountLiquidatedEvent)
			return c.handleAccountLiquidatedEvent(ev, ongoingSync)
		case abiparser.AccountEnabled:
			ev := e.Data.(abiparser.AccountEnabledEvent)
			return c.handleAccountEnabledEvent(ev, ongoingSync)
		default:
			c.logger.Debug("could not handle unknown event")
		}
		return nil, nil
	}
}

// handleValidatorAddedEvent handles registry contract event for validator added
func (c *controller) handleValidatorAddedEvent(
	validatorAddedEvent abiparser.ValidatorAddedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey)
	if ongoingSync {
		if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
			c.logger.Debug("validator was loaded already")
			return nil, nil
		}
	}

	metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
	validatorShare, found, err := c.collection.GetValidatorShare(validatorAddedEvent.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		validatorShare, _, err = c.onShareCreate(validatorAddedEvent)
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
		logFields = append(logFields, zap.String("validatorPubKey", pubKey))
	}
	return logFields, nil
}

// handleValidatorRemovedEvent handles registry contract event for validator removed
func (c *controller) handleValidatorRemovedEvent(
	validatorRemovedEvent abiparser.ValidatorRemovedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	// TODO: handle metrics
	validatorShare, found, err := c.collection.GetValidatorShare(validatorRemovedEvent.PublicKey)
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
		logFields = append(logFields, zap.String("validatorPubKey", validatorShare.PublicKey.SerializeToHexStr()))
	}

	return logFields, nil
}

// handleOperatorAddedEvent parses the given event and saves operator data
func (c *controller) handleOperatorAddedEvent(event abiparser.OperatorAddedEvent) ([]zap.Field, error) {
	eventOperatorPubKey := string(event.PublicKey)
	od := registrystorage.OperatorData{
		PublicKey:    eventOperatorPubKey,
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
		Index:        event.Id.Uint64(),
	}
	if err := c.storage.SaveOperatorData(&od); err != nil {
		return nil, errors.Wrap(err, "could not save operator data")
	}

	logFields := make([]zap.Field, 0)
	if strings.EqualFold(eventOperatorPubKey, c.operatorPubKey) {
		logFields = append(logFields,
			zap.String("operatorName", event.Name),
			zap.Uint64("operatorId", event.Id.Uint64()),
			zap.String("operatorPubKey", eventOperatorPubKey),
		)
	}
	exporter.ReportOperatorIndex(c.logger, &od)
	return logFields, nil
}

// handleOperatorRemovedEvent parses the given event and removing operator data
func (c *controller) handleOperatorRemovedEvent(
	event abiparser.OperatorRemovedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	od, found, err := c.storage.GetOperatorData(event.OperatorId.Uint64())
	if err != nil {
		return nil, errors.Wrap(err, "could not get operator data")
	}
	if !found {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find operator's data"),
		}
	}

	if od.OwnerAddress != event.OwnerAddress {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not match operator data owner address and index with provided event"),
		}
	}

	shares, err := c.collection.GetOperatorValidatorShares(c.operatorPubKey, false)
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

	err = c.storage.DeleteOperatorData(event.OperatorId.Uint64())
	if err != nil {
		return nil, errors.Wrap(err, "could not delete operator data")
	}

	logFields := make([]zap.Field, 0)
	if strings.EqualFold(od.PublicKey, c.operatorPubKey) {
		logFields = append(logFields,
			zap.String("operatorName", od.Name),
			zap.Uint64("operatorId", od.Index),
			zap.String("operatorPubKey", od.PublicKey),
		)
	}
	return logFields, nil
}

// handleAccountLiquidatedEvent handles registry contract event for account liquidated
func (c *controller) handleAccountLiquidatedEvent(
	event abiparser.AccountLiquidatedEvent,
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
				// due to the fact that after activating the validators (AccountEnabled)
				// we don't have the encrypted keys to decrypt the secret, but only the owner address
				if err := c.onShareRemove(share.PublicKey.SerializeToHexStr(), false); err != nil {
					return nil, err
				}
			}
		}
	}

	logFields := make([]zap.Field, 0)
	if len(operatorSharePubKeys) > 0 {
		logFields = append(logFields, zap.Strings("liquidatedShares", operatorSharePubKeys))
	}

	return logFields, nil
}

// handle AccountEnabledEvent handles registry contract event for account enabled
func (c *controller) handleAccountEnabledEvent(
	event abiparser.AccountEnabledEvent,
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
		logFields = append(logFields, zap.Strings("enabledShares", operatorSharePubKeys))
	}

	return logFields, nil
}
