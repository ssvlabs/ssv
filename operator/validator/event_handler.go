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
	"github.com/bloxapp/ssv/protocol/v2/types"
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
		case abiparser.ClusterLiquidated:
			ev := e.Data.(abiparser.ClusterLiquidatedEvent)
			return c.handleClusterLiquidatedEvent(logger, ev, ongoingSync)
		case abiparser.ClusterReactivated:
			ev := e.Data.(abiparser.ClusterReactivatedEvent)
			return c.handleClusterReactivatedEvent(logger, ev, ongoingSync)
		case abiparser.FeeRecipientAddressUpdated:
			ev := e.Data.(abiparser.FeeRecipientAddressUpdatedEvent)
			return c.handleFeeRecipientAddressUpdatedEvent(logger, ev, ongoingSync)
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
		c.operatorData.ID != event.ID {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("operator registered with the same operator public key"),
		}
	}

	isOperatorEvent := false
	od := &registrystorage.OperatorData{
		PublicKey:    event.PublicKey,
		OwnerAddress: event.Owner,
		ID:           event.ID,
	}
	if err := c.operatorsStorage.SaveOperatorData(logger, od); err != nil {
		return nil, errors.Wrap(err, "could not save operator data")
	}

	if bytes.Equal(event.PublicKey, c.operatorData.PublicKey) {
		isOperatorEvent = true
		c.operatorData = od
	}

	logFields := make([]zap.Field, 0)
	if isOperatorEvent || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.Uint64("operatorId", od.ID),
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
	od, found, err := c.operatorsStorage.GetOperatorData(event.ID)
	if err != nil {
		return nil, errors.Wrap(err, "could not get operator data")
	}
	if !found {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find operator data"),
		}
	}

	logFields := make([]zap.Field, 0)
	logFields = append(logFields,
		zap.Uint64("operatorId", od.ID),
		zap.String("operatorPubKey", string(od.PublicKey)),
		zap.String("ownerAddress", od.OwnerAddress.String()),
	)

	return logFields, nil
}

// handleValidatorAddedEvent handles registry contract event for validator added
func (c *controller) handleValidatorAddedEvent(
	logger *zap.Logger,
	event abiparser.ValidatorAddedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	pubKey := hex.EncodeToString(event.PublicKey)
	// TODO: check if need
	if ongoingSync {
		if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
			logger.Debug("validator was loaded already")
			return nil, nil
		}
	}

	validatorShare, found, err := c.sharesStorage.GetShare(event.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		validatorShare, err = c.onShareCreate(logger, event)
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
			_, err = c.onShareStart(logger, validatorShare)
			if err != nil {
				logger.Warn("could not start validator", zap.String("pubkey", hex.EncodeToString(validatorShare.ValidatorPubKey)), zap.Error(err))
			}
		}
	}

	if isOperatorShare || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.String("validatorPubKey", pubKey),
			zap.String("ownerAddress", validatorShare.OwnerAddress.String()),
			zap.Uint64s("operatorIds", event.OperatorIds),
		)
	}

	return logFields, nil
}

// handleValidatorRemovedEvent handles registry contract event for validator removed
func (c *controller) handleValidatorRemovedEvent(
	logger *zap.Logger,
	event abiparser.ValidatorRemovedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	// TODO: handle metrics
	share, found, err := c.sharesStorage.GetShare(event.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find validator share"),
		}
	}

	// remove decided messages
	messageID := spectypes.NewMsgID(types.GetDefaultDomain(), share.ValidatorPubKey, spectypes.BNRoleAttester)
	store := c.ibftStorageMap.Get(messageID.GetRoleType())
	if store != nil {
		if err := store.CleanAllInstances(logger, messageID[:]); err != nil { // TODO need to delete for multi duty as well
			return nil, errors.Wrap(err, "could not clean all decided messages")
		}
	}

	// remove from storage
	if err := c.sharesStorage.DeleteShare(share.ValidatorPubKey); err != nil {
		return nil, errors.Wrap(err, "could not remove validator share")
	}

	isOperatorShare := share.BelongsToOperator(c.operatorData.ID)
	if isOperatorShare {
		pubKey := hex.EncodeToString(event.PublicKey)
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusRemoved))
		if ongoingSync {
			if err := c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), true); err != nil {
				return nil, err
			}
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

// handleClusterLiquidatedEvent handles registry contract event for cluster liquidated
func (c *controller) handleClusterLiquidatedEvent(
	logger *zap.Logger,
	event abiparser.ClusterLiquidatedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	toLiquidate, liquidatedPubKeys, err := c.processClusterEvent(logger, event.Owner, event.OperatorIds, true)
	if err != nil {
		return nil, errors.Wrapf(err, "could not process cluster event")
	}

	if ongoingSync && len(toLiquidate) > 0 {
		for _, share := range toLiquidate {
			// we can't remove the share secret from key-manager
			// due to the fact that after activating the validators (ClusterReactivated)
			// we don't have the encrypted keys to decrypt the secret, but only the owner address
			if err = c.onShareRemove(hex.EncodeToString(share.ValidatorPubKey), false); err != nil {
				return nil, err
			}
		}
	}

	logFields := make([]zap.Field, 0)
	if len(liquidatedPubKeys) > 0 {
		logFields = append(logFields,
			zap.String("ownerAddress", event.Owner.String()),
			zap.Strings("liquidatedShares", liquidatedPubKeys),
		)
	}

	return logFields, nil
}

// handle ClusterReactivatedEvent handles registry contract event for cluster enabled
func (c *controller) handleClusterReactivatedEvent(
	logger *zap.Logger,
	event abiparser.ClusterReactivatedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	toEnable, enabledPubKeys, err := c.processClusterEvent(logger, event.Owner, event.OperatorIds, false)
	if err != nil {
		return nil, errors.Wrapf(err, "could not process cluster event")
	}

	if ongoingSync && len(toEnable) > 0 {
		for _, share := range toEnable {
			_, err = c.onShareStart(logger, share)
			if err != nil {
				logger.Warn("could not start validator", zap.String("pubkey", hex.EncodeToString(share.ValidatorPubKey)), zap.Error(err))
			}
		}
	}

	logFields := make([]zap.Field, 0)
	if len(enabledPubKeys) > 0 {
		logFields = append(logFields,
			zap.String("ownerAddress", event.Owner.String()),
			zap.Strings("enabledShares", enabledPubKeys),
		)
	}

	return logFields, nil
}

// processClusterEvent handles registry contract event for cluster
func (c *controller) processClusterEvent(
	logger *zap.Logger,
	owner common.Address,
	operatorIDs []uint64,
	toLiquidate bool,
) ([]*types.SSVShare, []string, error) {
	clusterID, err := types.ComputeClusterIDHash(owner.Bytes(), operatorIDs)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "could not compute share cluster id")
	}

	shares, err := c.sharesStorage.GetFilteredShares(logger, registrystorage.ByClusterID(clusterID))
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not get validator shares by cluster id")
	}
	toUpdate := make([]*types.SSVShare, 0)
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
		if err = c.sharesStorage.SaveShareMany(logger, toUpdate); err != nil {
			return nil, nil, errors.Wrapf(err, "could not save validator shares")
		}
	}

	return toUpdate, updatedPubKeys, nil
}

func (c *controller) handleFeeRecipientAddressUpdatedEvent(
	logger *zap.Logger,
	event abiparser.FeeRecipientAddressUpdatedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	recipientData := &registrystorage.RecipientData{
		Owner: event.Owner,
	}
	copy(recipientData.FeeRecipient[:], event.RecipientAddress.Bytes())
	r, err := c.recipientsStorage.SaveRecipientData(recipientData)
	if err != nil {
		return nil, errors.Wrap(err, "could not save recipient data")
	}

	if ongoingSync && r != nil {
		_ = c.validatorsMap.ForEach(func(v *validator.Validator) error {
			if bytes.Equal(v.Share.OwnerAddress.Bytes(), r.Owner.Bytes()) {
				v.Share.FeeRecipient = r.FeeRecipient
			}
			return nil
		})
	}

	var isOperatorEvent bool
	if c.operatorData.ID != 0 {
		shares, err := c.sharesStorage.GetFilteredShares(logger, registrystorage.ByOperatorID(c.operatorData.ID))
		if err != nil {
			return nil, errors.Wrap(err, "could not get validator shares by operator id")
		}
		for _, share := range shares {
			if bytes.Equal(share.OwnerAddress.Bytes(), event.Owner.Bytes()) {
				isOperatorEvent = true
				break
			}
		}
	}

	logFields := make([]zap.Field, 0)
	if isOperatorEvent || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.String("ownerAddress", event.Owner.String()),
			zap.String("feeRecipient", event.RecipientAddress.String()),
		)
	}

	return logFields, nil
}
