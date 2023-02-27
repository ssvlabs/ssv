package validator

import (
	"bytes"
	"encoding/hex"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

// Eth1EventHandler is a factory function for creating eth1 event handler
func (c *controller) Eth1EventHandler(logger *zap.Logger, ongoingSync bool) eth1.SyncEventHandler {
	return func(e eth1.Event) ([]zap.Field, error) {
		switch e.Name {
		case abiparser.OperatorAdded:
			ev := e.Data.(abiparser.OperatorAddedEvent)
			return c.handleOperatorAddedEvent(logger, ev)
		case abiparser.OperatorRemoved:
			ev := e.Data.(abiparser.OperatorRemovedEvent)
			return c.handleOperatorRemovedEvent(logger, ev, ongoingSync)
		case abiparser.ValidatorAdded:
			ev := e.Data.(abiparser.ValidatorAddedEvent)
			return c.handleValidatorAddedEvent(logger, ev, ongoingSync)
		case abiparser.ValidatorRemoved:
			ev := e.Data.(abiparser.ValidatorRemovedEvent)
			return c.handleValidatorRemovedEvent(logger, ev, ongoingSync)
		case abiparser.PodLiquidated:
			ev := e.Data.(abiparser.PodLiquidatedEvent)
			return c.handlePodLiquidatedEvent(logger, ev, ongoingSync)
		case abiparser.PodEnabled:
			ev := e.Data.(abiparser.PodEnabledEvent)
			return c.handlePodEnabledEvent(ev, ongoingSync)
		case abiparser.FeeRecipientAddressAdded:
			ev := e.Data.(abiparser.FeeRecipientAddressAddedEvent)
			return c.handleFeeRecipientAddressAddedEvent(logger, ev, ongoingSync)
		default:
			logger.Debug("could not handle unknown event")
		}
		return nil, nil
	}
}

// handleOperatorAddedEvent parses the given event and saves operator data
func (c *controller) handleOperatorAddedEvent(logger *zap.Logger, event abiparser.OperatorAddedEvent) ([]zap.Field, error) {
	// throw an error if there is an existing operator with the same public key and different operator id
	if c.operatorData.ID != 0 && bytes.Equal(c.operatorData.PublicKey, event.PublicKey) &&
		c.operatorData.ID != spectypes.OperatorID(event.Id) {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("operator registered with the same operator public key"),
		}
	}

	isOperatorEvent := false
	od := &registrystorage.OperatorData{
		PublicKey:    event.PublicKey,
		OwnerAddress: event.Owner,
		ID:           spectypes.OperatorID(event.Id),
	}
	if err := c.operatorsCollection.SaveOperatorData(logger, od); err != nil {
		return nil, errors.Wrap(err, "could not save operator data")
	}

	if bytes.Equal(event.PublicKey, c.operatorData.PublicKey) {
		isOperatorEvent = true
		c.operatorData = od
	}

	logFields := make([]zap.Field, 0)
	if isOperatorEvent || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.Uint64("operatorId", uint64(od.ID)),
			zap.String("operatorPubKey", string(od.PublicKey)),
			zap.String("ownerAddress", od.OwnerAddress.String()),
		)
	}
	exporter.ReportOperatorIndex(logger, od)
	return logFields, nil
}

// handleOperatorRemovedEvent parses the given event and removing operator data
func (c *controller) handleOperatorRemovedEvent(
	logger *zap.Logger,
	event abiparser.OperatorRemovedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	od, found, err := c.operatorsCollection.GetOperatorData(spectypes.OperatorID(event.Id))
	if err != nil {
		return nil, errors.Wrap(err, "could not get operator data")
	}
	if !found {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find operator data"),
		}
	}

	isOperatorEvent := od.ID == c.operatorData.ID
	logFields := make([]zap.Field, 0)
	if isOperatorEvent || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.Uint64("operatorId", uint64(od.ID)),
			zap.String("operatorPubKey", string(od.PublicKey)),
			zap.String("ownerAddress", od.OwnerAddress.String()),
		)
	}

	if !isOperatorEvent {
		// TODO: remove this check when we will support operator removal for non-operator (mark as inactive)
		return logFields, nil
	}

	shares, err := c.collection.GetFilteredValidatorShares(logger, ByOperatorID(spectypes.OperatorID(event.Id)))
	if err != nil {
		return nil, errors.Wrap(err, "could not get all operator validator shares")
	}

	// TODO: delete many
	for _, share := range shares {
		if err := c.collection.DeleteValidatorShare(share.ValidatorPubKey); err != nil {
			return nil, errors.Wrap(err, "could not remove validator share")
		}
		if ongoingSync {
			if err := c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), true); err != nil {
				return nil, err
			}
		}
	}

	err = c.operatorsCollection.DeleteOperatorData(spectypes.OperatorID(event.Id))
	if err != nil {
		return nil, errors.Wrap(err, "could not delete operator data")
	}

	return logFields, nil
}

// handleValidatorAddedEvent handles registry contract event for validator added
func (c *controller) handleValidatorAddedEvent(
	logger *zap.Logger,
	validatorAddedEvent abiparser.ValidatorAddedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey)
	// TODO: check if need
	if ongoingSync {
		if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
			logger.Debug("validator was loaded already")
			return nil, nil
		}
	}

	validatorShare, found, err := c.collection.GetValidatorShare(validatorAddedEvent.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		validatorShare, err = c.onShareCreate(logger, validatorAddedEvent)
		if err != nil {
			metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusError))
			return nil, err
		}
	}

	logFields := make([]zap.Field, 0)
	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if isOperatorShare {
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
		if ongoingSync {
			// TODO: should we handle error?
			_, _ = c.onShareStart(logger, validatorShare)
		}
	}

	if isOperatorShare || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.String("validatorPubKey", pubKey),
			zap.String("ownerAddress", validatorShare.OwnerAddress.String()),
			zap.Uint64s("operatorIds", validatorAddedEvent.OperatorIds),
		)
	}

	return logFields, nil
}

// handleValidatorRemovedEvent handles registry contract event for validator removed
func (c *controller) handleValidatorRemovedEvent(
	logger *zap.Logger,
	validatorRemovedEvent abiparser.ValidatorRemovedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	// TODO: handle metrics
	share, found, err := c.collection.GetValidatorShare(validatorRemovedEvent.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find validator share"),
		}
	}

	// remove decided messages
	messageID := spectypes.NewMsgID(share.ValidatorPubKey, spectypes.BNRoleAttester)
	store := c.ibftStorageMap.Get(messageID.GetRoleType())
	if store != nil {
		if err := store.CleanAllInstances(logger, messageID[:]); err != nil { // TODO need to delete for multi duty as well
			return nil, errors.Wrap(err, "could not clean all decided messages")
		}
	}

	// remove from storage
	if err := c.collection.DeleteValidatorShare(share.ValidatorPubKey); err != nil {
		return nil, errors.Wrap(err, "could not remove validator share")
	}

	isOperatorShare := share.BelongsToOperator(c.operatorData.ID)
	if isOperatorShare {
		pubKey := hex.EncodeToString(validatorRemovedEvent.PublicKey)
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusRemoved))
		if ongoingSync {
			if err := c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), true); err != nil {
				return nil, err
			}
		}
	}

	// remove recipient if there are no more validators under the removed validator owner address
	shares, err := c.collection.GetFilteredValidatorShares(ByOwnerAddress(share.OwnerAddress))
	if err != nil {
		return nil, errors.Wrap(err, "could not get validator shares by owner address")
	}
	if len(shares) == 0 {
		if err := c.recipientsCollection.DeleteRecipientData(validatorRemovedEvent.OwnerAddress); err != nil {
			return nil, errors.Wrap(err, "could not delete recipient")
		}
	}

	logFields := make([]zap.Field, 0)
	if isOperatorShare || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.String("validatorPubKey", hex.EncodeToString(share.ValidatorPubKey)),
			zap.String("ownerAddress", share.OwnerAddress.String()),
		)
	}

	return logFields, nil
}

// handlePodLiquidatedEvent handles registry contract event for pod liquidated
func (c *controller) handlePodLiquidatedEvent(
	logger *zap.Logger,
	event abiparser.PodLiquidatedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	toLiquidate, liquidatedPubKeys, err := c.processPodEvent(event.OwnerAddress, event.OperatorIds, true)
	if err != nil {
		return nil, errors.Wrapf(err, "could not process pod event")
	}

	if ongoingSync && len(toLiquidate) > 0 {
		for _, share := range toLiquidate {
			// we can't remove the share secret from key-manager
			// due to the fact that after activating the validators (PodEnabled)
			// we don't have the encrypted keys to decrypt the secret, but only the owner address
			if err = c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), false); err != nil {
				return nil, err
			}
		}
	}

	logFields := make([]zap.Field, 0)
	if len(liquidatedPubKeys) > 0 {
		logFields = append(logFields,
			zap.String("ownerAddress", event.OwnerAddress.String()),
			zap.Strings("liquidatedShares", liquidatedPubKeys),
		)
	}

	return logFields, nil
}

// handle PodEnabledEvent handles registry contract event for pod enabled
func (c *controller) handlePodEnabledEvent(
	logger *zap.Logger,
	event abiparser.PodEnabledEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	toEnable, enabledPubKeys, err := c.processPodEvent(event.OwnerAddress, event.OperatorIds, false)
	if err != nil {
		return nil, errors.Wrapf(err, "could not process pod event")
	}

	if ongoingSync && len(toEnable) > 0 {
		for _, share := range toEnable {
			_, _ = c.onShareStart(logger, share)
		}
	}

	// TODO(oleg): update km with minimal slashing protection

	logFields := make([]zap.Field, 0)
	if len(enabledPubKeys) > 0 {
		logFields = append(logFields,
			zap.String("ownerAddress", event.OwnerAddress.String()),
			zap.Strings("enabledShares", enabledPubKeys),
		)
	}

	return logFields, nil
}

// processPodEvent handles registry contract event for pod
func (c *controller) processPodEvent(
	owner common.Address,
	operatorIds []uint64,
	toLiquidate bool,
) ([]*ssvtypes.SSVShare, []string, error) {
	podID, err := ssvtypes.ComputePodIDHash(owner.Bytes(), operatorIds)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "could not compute share pod id")
	}

	shares, err := c.collection.GetFilteredValidatorShares(ByPodID(podID))
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not get validator shares by pod id")
	}
	toUpdate := make([]*ssvtypes.SSVShare, 0)
	updatedPubKeys := make([]string, 0)

	for _, share := range shares {
		isOperatorShare := share.BelongsToOperator(c.operatorData.ID)
		if isOperatorShare || c.validatorOptions.FullNode {
			updatedPubKeys = append(updatedPubKeys, hex.EncodeToString(share.ValidatorPubKey))
		}
		if isOperatorShare {
			share.Liquidated = toLiquidate
			toUpdate = append(toUpdate, share)
		}
	}

	if len(toUpdate) > 0 {
		if err = c.collection.SaveValidatorShares(toUpdate); err != nil {
			return nil, nil, errors.Wrapf(err, "could not save validator shares")
		}
	}

	return toUpdate, updatedPubKeys, nil
}

func (c *controller) handleFeeRecipientAddressAddedEvent(
	event abiparser.FeeRecipientAddressAddedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	r, err := c.recipientsCollection.SaveRecipientData(&registrystorage.RecipientData{
		Owner: event.OwnerAddress,
		Fee:   event.RecipientAddress,
	})
	if err != nil {
		return nil, errors.Wrap(err, "could not save recipient data")
	}

	if ongoingSync && r != nil {
		_ = c.validatorsMap.ForEach(func(v *validator.Validator) error {
			if bytes.Equal(v.Share.OwnerAddress.Bytes(), r.Owner.Bytes()) {
				v.Share.FeeRecipient = r.Fee
			}
			return nil
		})
	}

	return nil, nil
}
