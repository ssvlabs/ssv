package validator

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

// b64 encrypted key length is 256
const encryptedKeyLength = 256

func splitBytes(buf []byte, lim int) [][]byte {
	var chunk []byte
	chunks := make([][]byte, 0, len(buf)/lim+1)
	for len(buf) >= lim {
		chunk, buf = buf[:lim], buf[lim:]
		chunks = append(chunks, chunk)
	}
	if len(buf) > 0 {
		chunks = append(chunks, buf[:])
	}
	return chunks
}

// Eth1EventHandler is a factory function for creating eth1 event handler
func (c *controller) Eth1EventHandler(logger *zap.Logger, ongoingSync bool) eth1.SyncEventHandler {
	return func(e eth1.Event) ([]zap.Field, error) {
		switch ev := e.Data.(type) {
		case abiparser.OperatorAddedEvent:
			return c.handleOperatorAddedEvent(logger, ev)
		case abiparser.OperatorRemovedEvent:
			return c.handleOperatorRemovedEvent(logger, ev, ongoingSync)
		case abiparser.ValidatorAddedEvent:
			return c.handleValidatorAddedEvent(logger, ev, ongoingSync)
		case abiparser.ValidatorRemovedEvent:
			return c.handleValidatorRemovedEvent(logger, ev, ongoingSync)
		case abiparser.ClusterLiquidatedEvent:
			return c.handleClusterLiquidatedEvent(logger, ev, ongoingSync)
		case abiparser.ClusterReactivatedEvent:
			return c.handleClusterReactivatedEvent(logger, ev, ongoingSync)
		case abiparser.FeeRecipientAddressUpdatedEvent:
			return c.handleFeeRecipientAddressUpdatedEvent(logger, ev, ongoingSync)
		default:
			logger.Debug("could not handle unknown event",
				zap.String("event_name", e.Name),
				zap.String("event_type", fmt.Sprintf("%T", ev)),
			)
		}
		return nil, nil
	}
}

// handleOperatorAddedEvent parses the given event and saves operator data
func (c *controller) handleOperatorAddedEvent(logger *zap.Logger, event abiparser.OperatorAddedEvent) ([]zap.Field, error) {
	// throw an error if there is an existing operator with the same public key and different operator id
	if c.operatorData.ID != 0 && bytes.Equal(c.operatorData.PublicKey, event.PublicKey) &&
		c.operatorData.ID != event.OperatorId {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("operator registered with the same operator public key"),
		}
	}

	isOperatorEvent := false
	od := &registrystorage.OperatorData{
		PublicKey:    event.PublicKey,
		OwnerAddress: event.Owner,
		ID:           event.OperatorId,
	}

	exists, err := c.operatorsStorage.SaveOperatorData(logger, od)
	if err != nil {
		return nil, errors.Wrap(err, "could not save operator data")
	}
	if exists {
		return nil, nil
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
	od, found, err := c.operatorsStorage.GetOperatorData(event.OperatorId)
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
) (logFields []zap.Field, err error) {
	var valid bool
	defer func() {
		err = c.handleValidatorAddedEventDefer(valid, err, event)
	}()

	_, found, eventErr := c.eventHandler.GetEventData(event.TxHash)
	if eventErr != nil {
		return nil, errors.Wrap(eventErr, "failed to get event data")
	}
	if found {
		// skip
		return nil, nil
	}

	// get nonce
	nonce, nonceErr := c.eventHandler.GetNextNonce(event.Owner)
	if nonceErr != nil {
		return nil, errors.Wrap(nonceErr, "failed to get next nonce")
	}

	// Calculate the expected length of constructed shares based on the number of operator IDs,
	// signature length, public key length, and encrypted key length.
	operatorCount := len(event.OperatorIds)
	signatureOffset := phase0.SignatureLength
	pubKeysOffset := phase0.PublicKeyLength*operatorCount + signatureOffset
	sharesExpectedLength := encryptedKeyLength*operatorCount + pubKeysOffset

	if sharesExpectedLength != len(event.Shares) {
		err = &abiparser.MalformedEventError{
			Err: errors.Errorf(
				"%s event shares length is not correct: expected %d, got %d",
				abiparser.ValidatorAdded,
				sharesExpectedLength,
				len(event.Shares),
			),
		}
		return nil, err
	}

	event.Signature = event.Shares[:signatureOffset]
	event.SharePublicKeys = splitBytes(event.Shares[signatureOffset:pubKeysOffset], phase0.PublicKeyLength)
	event.EncryptedKeys = splitBytes(event.Shares[pubKeysOffset:], len(event.Shares[pubKeysOffset:])/operatorCount)

	// verify sig
	if err = verifySignature(event.Signature, event.Owner, event.PublicKey, nonce); err != nil {
		err = &abiparser.MalformedEventError{Err: errors.Wrap(err, "failed to verify signature")}
		return nil, err
	}

	pubKey := hex.EncodeToString(event.PublicKey)
	// TODO: check if need
	if ongoingSync {
		if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
			logger.Debug("validator was loaded already")
			return nil, nil
		}
	}

	validatorShare := c.sharesStorage.Get(event.PublicKey)
	if validatorShare == nil {
		validatorShare, err = c.onShareCreate(logger, event)
		if err != nil {
			metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusError))
			return nil, err
		}
		valid = true
	} else if event.Owner != validatorShare.OwnerAddress {
		// Prevent multiple registration of the same validator with different owner address
		// owner A registers validator with public key X (OK)
		// owner B registers validator with public key X (NOT OK)
		err = &abiparser.MalformedEventError{
			Err: errors.Errorf(
				"validator share already exists with different owner address: expected %s, got %s",
				validatorShare.OwnerAddress.String(),
				event.Owner.String(),
			),
		}
		return nil, err
	}

	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if isOperatorShare {
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
		if ongoingSync {
			if _, startErr := c.onShareStart(logger, validatorShare); startErr != nil {
				logger.Warn("could not start validator",
					zap.String("pubkey", hex.EncodeToString(validatorShare.ValidatorPubKey)),
					zap.Error(startErr),
				)
			}
		}
	}

	logFields = make([]zap.Field, 0)
	if isOperatorShare || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.String("validatorPubKey", pubKey),
			zap.String("ownerAddress", validatorShare.OwnerAddress.String()),
			zap.Uint64s("operatorIds", event.OperatorIds),
		)
	}

	return logFields, err
}

func (c *controller) handleValidatorAddedEventDefer(valid bool, err error, event abiparser.ValidatorAddedEvent) error {
	var malformedEventErr *abiparser.MalformedEventError

	if valid || errors.As(err, &malformedEventErr) {
		saveErr := c.eventHandler.SaveEventData(event.TxHash)
		if saveErr != nil {
			wrappedErr := errors.Wrap(saveErr, "could not save event data")
			if err == nil {
				return wrappedErr
			}
			return errors.Wrap(err, wrappedErr.Error())
		}

		bumpErr := c.eventHandler.BumpNonce(event.Owner)
		if bumpErr != nil {
			wrappedErr := errors.Wrap(bumpErr, "failed to bump the nonce")
			if err == nil {
				return wrappedErr
			}
			return errors.Wrap(err, wrappedErr.Error())
		}
	}

	return err
}

// handleValidatorRemovedEvent handles registry contract event for validator removed
func (c *controller) handleValidatorRemovedEvent(
	logger *zap.Logger,
	event abiparser.ValidatorRemovedEvent,
	ongoingSync bool,
) ([]zap.Field, error) {
	// TODO: handle metrics
	share := c.sharesStorage.Get(event.PublicKey)
	if share == nil {
		return nil, &abiparser.MalformedEventError{
			Err: errors.New("could not find validator share"),
		}
	}

	// Prevent removal of the validator registered with different owner address
	// owner A registers validator with public key X (OK)
	// owner B registers validator with public key X (NOT OK)
	// owner A removes validator with public key X (OK)
	// owner B removes validator with public key X (NOT OK)
	if event.Owner != share.OwnerAddress {
		return nil, &abiparser.MalformedEventError{
			Err: errors.Errorf(
				"validator share already exists with different owner address: expected %s, got %s",
				share.OwnerAddress.String(),
				event.Owner.String(),
			),
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
	if err := c.sharesStorage.Delete(share.ValidatorPubKey); err != nil {
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
			zap.Strings("liquidatedValidators", liquidatedPubKeys),
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
			zap.Strings("enabledValidators", enabledPubKeys),
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

	shares := c.sharesStorage.List(registrystorage.ByClusterID(clusterID))
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
		if err = c.sharesStorage.Save(logger, toUpdate...); err != nil {
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
			if v.Share.OwnerAddress == r.Owner {
				v.Share.FeeRecipientAddress = r.FeeRecipient
			}
			return nil
		})
	}

	var isOperatorEvent bool
	if c.operatorData.ID != 0 {
		shares := c.sharesStorage.List(registrystorage.ByOperatorID(c.operatorData.ID))
		for _, share := range shares {
			if share.OwnerAddress == event.Owner {
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

// todo(align-contract-v0.3.1-rc.0): move to crypto package in ssv protocol?
// verify signature of the ValidatorAddedEvent shares data
func verifySignature(sig []byte, owner common.Address, pubKey []byte, nonce registrystorage.Nonce) error {
	data := fmt.Sprintf("%s:%d", owner.String(), nonce)
	hash := crypto.Keccak256([]byte(data))

	sign := &bls.Sign{}
	if err := sign.Deserialize(sig); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	pk := &bls.PublicKey{}
	if err := pk.Deserialize(pubKey); err != nil {
		return errors.Wrap(err, "failed to deserialize public key")
	}

	if res := sign.VerifyByte(pk, hash); !res {
		return errors.New("failed to verify signature")
	}

	return nil
}
