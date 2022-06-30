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

// ErrorNotFound is returned when the validator share is not found
type ErrorNotFound struct {
	Err error
}

func (e *ErrorNotFound) Error() string {
	return e.Err.Error()
}

// Eth1EventHandler is a factory function for creating eth1 event handler
func (c *controller) Eth1EventHandler(ongoingSync bool) eth1.SyncEventHandler {
	return func(e eth1.Event) error {
		switch e.Name {
		case abiparser.OperatorAdded:
			ev := e.Data.(abiparser.OperatorAddedEvent)
			if strings.EqualFold(string(ev.PublicKey), c.operatorPubKey) {
				c.logger.Debug("My Operator Event",
					zap.String("pubKey", string(ev.PublicKey)),
					zap.String("name", ev.Name),
					zap.Uint64("ID", ev.Id.Uint64()),
					zap.Uint64("blockNumber", e.Log.BlockNumber),
					zap.String("txHash", e.Log.TxHash.Hex()),
				)
			}
			return c.handleOperatorAddedEvent(ev)
		case abiparser.OperatorRemoved:
			// TODO: should we stop all validators owned by this deleted operator?
			ev := e.Data.(abiparser.OperatorRemovedEvent)
			return c.handleOperatorRemovedEvent(ev)
		case abiparser.ValidatorAdded:
			ev := e.Data.(abiparser.ValidatorAddedEvent)
			pubKey := hex.EncodeToString(ev.PublicKey)
			if ongoingSync {
				if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
					c.logger.Debug("validator was loaded already")
					return nil
				}
			}
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
		return nil
	}
}

// handleValidatorAddedEvent handles registry contract event for validator added
func (c *controller) handleValidatorAddedEvent(
	validatorAddedEvent abiparser.ValidatorAddedEvent,
	ongoingSync bool,
) error {
	pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey)
	metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
	validatorShare, found, err := c.collection.GetValidatorShare(validatorAddedEvent.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		validatorShare, _, err = c.onShareCreate(validatorAddedEvent)
		if err != nil {
			metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusError))
			return err
		}
	}

	isOperatorShare := validatorShare.IsOperatorShare(c.operatorPubKey)
	if isOperatorShare {
		logger := c.logger.With(zap.String("pubKey", pubKey))
		logger.Debug("ValidatorAdded event was handled successfully")
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
		if ongoingSync {
			c.onShareStart(validatorShare)
		}
	}
	return nil
}

// handleValidatorRemovedEvent handles registry contract event for validator removed
func (c *controller) handleValidatorRemovedEvent(
	validatorRemovedEvent abiparser.ValidatorRemovedEvent,
	ongoingSync bool,
) error {
	// TODO: handle metrics
	validatorShare, found, err := c.collection.GetValidatorShare(validatorRemovedEvent.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		return &abiparser.MalformedEventError{
			Err: errors.New("could not find validator share"),
		}
	}

	// remove from storage
	if err := c.collection.DeleteValidatorShare(validatorShare.PublicKey.Serialize()); err != nil {
		return errors.Wrap(err, "could not remove validator share")
	}

	if ongoingSync {
		// determine if validator share belongs to operator
		isOperatorShare := validatorShare.IsOperatorShare(c.operatorPubKey)
		if isOperatorShare {
			if err := c.onShareRemove(validatorShare.PublicKey.SerializeToHexStr(), true); err != nil {
				return err
			}
		}
	}

	return nil
}

// handleOperatorAddedEvent parses the given event and saves operator data
func (c *controller) handleOperatorAddedEvent(event abiparser.OperatorAddedEvent) error {
	eventOperatorPubKey := string(event.PublicKey)
	od := registrystorage.OperatorData{
		PublicKey:    eventOperatorPubKey,
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
		Index:        event.Id.Uint64(),
	}
	if err := c.storage.SaveOperatorData(&od); err != nil {
		return errors.Wrap(err, "could not save operator data")
	}

	exporter.ReportOperatorIndex(c.logger, &od)
	return nil
}

// handleOperatorRemovedEvent parses the given event and removing operator data
func (c *controller) handleOperatorRemovedEvent(event abiparser.OperatorRemovedEvent) error {
	od, found, err := c.storage.GetOperatorData(event.OperatorId.Uint64())
	if err != nil {
		return errors.Wrap(err, "could not get operator's data")
	}
	if !found {
		return &abiparser.MalformedEventError{
			Err: errors.New("could not find operator's data"),
		}
	}

	if od.OwnerAddress != event.OwnerAddress {
		return &abiparser.MalformedEventError{
			Err: errors.New("could not match operator's data owner address and index with provided event"),
		}
	}

	err = c.storage.DeleteOperatorData(event.OperatorId.Uint64())
	if err != nil {
		return errors.Wrap(err, "could not delete operator's data")
	}

	return nil
}

// handleAccountLiquidatedEvent handles registry contract event for account liquidated
func (c *controller) handleAccountLiquidatedEvent(
	event abiparser.AccountLiquidatedEvent,
	ongoingSync bool,
) error {
	ownerAddress := event.OwnerAddress.String()
	shares, err := c.collection.GetValidatorSharesByOwnerAddress(ownerAddress)
	if err != nil {
		return errors.Wrap(err, "could not get validator shares by owner address")
	}

	for _, share := range shares {
		if share.IsOperatorShare(c.operatorPubKey) {
			share.Liquidated = true

			// save validator data
			if err := c.collection.SaveValidatorShare(share); err != nil {
				return errors.Wrap(err, "could not save validator share")
			}

			if ongoingSync {
				// we can't remove the share secret from key-manager
				// due to the fact that after activating the validators (AccountEnabled)
				// we don't have the encrypted keys to decrypt the secret, but only the owner address
				if err := c.onShareRemove(share.PublicKey.SerializeToHexStr(), false); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// handle AccountEnabledEvent handles registry contract event for account enabled
func (c *controller) handleAccountEnabledEvent(
	event abiparser.AccountEnabledEvent,
	ongoingSync bool,
) error {
	ownerAddress := event.OwnerAddress.String()
	shares, err := c.collection.GetValidatorSharesByOwnerAddress(ownerAddress)
	if err != nil {
		return errors.Wrap(err, "could not get validator shares by owner address")
	}

	for _, share := range shares {
		if share.IsOperatorShare(c.operatorPubKey) {
			share.Liquidated = false

			// save validator data
			if err := c.collection.SaveValidatorShare(share); err != nil {
				return errors.Wrap(err, "could not save validator share")
			}

			if ongoingSync {
				c.onShareStart(share)
			}
		}
	}

	return nil
}
