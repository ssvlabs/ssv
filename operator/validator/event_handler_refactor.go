package validator

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/eth1_refactor/contract"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/logging/fields"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// TODO: consider extracting from controller

func (c *controller) HandleOperatorAdded(event *contract.ContractOperatorAdded) error {
	od := &registrystorage.OperatorData{
		PublicKey:    event.PublicKey,
		OwnerAddress: event.Owner,
		ID:           event.OperatorId,
	}

	logger := c.defaultLogger.With(
		fields.OperatorID(od.ID),
		// TODO: move to fields package
		zap.String("operator_pub_key", string(od.PublicKey)),
		zap.String("owner_address", od.OwnerAddress.String()),
	)

	logger.Info("processing OperatorAddedEvent")

	// throw an error if there is an existing operator with the same public key and different operator id
	if c.operatorData.ID != 0 && bytes.Equal(c.operatorData.PublicKey, event.PublicKey) &&
		c.operatorData.ID != event.OperatorId {
		return &abiparser.MalformedEventError{
			Err: fmt.Errorf("operator registered with the same operator public key"),
		}
	}

	exists, err := c.operatorsStorage.SaveOperatorData(logger, od)
	if err != nil {
		return fmt.Errorf("could not save operator data: %w", err)
	}
	if exists {
		return nil
	}

	if bytes.Equal(event.PublicKey, c.operatorData.PublicKey) {
		c.operatorData = od
	}

	exporter.ReportOperatorIndex(logger, od)

	logger.Info("processed OperatorAddedEvent")

	return nil
}

func (c *controller) HandleOperatorRemoved(event *contract.ContractOperatorRemoved) error {
	c.defaultLogger.Info("processing OperatorRemovedEvent") // TODO: consider adding more fields

	od, found, err := c.operatorsStorage.GetOperatorData(event.OperatorId)
	if err != nil {
		return fmt.Errorf("could not get operator data: %w", err)
	}
	if !found {
		return &abiparser.MalformedEventError{
			Err: fmt.Errorf("could not find operator data"),
		}
	}

	c.defaultLogger.With(
		zap.Uint64("operatorId", od.ID),
		zap.String("operatorPubKey", string(od.PublicKey)),
		zap.String("ownerAddress", od.OwnerAddress.String()),
	).Info("processed OperatorRemovedEvent")

	return nil
}

func (c *controller) HandleValidatorAdded(event *contract.ContractValidatorAdded) (err error) {
	var valid bool
	defer func() {
		err = c.validatorAddedDefer(valid, err, event)
	}()

	_, found, eventErr := c.eventHandler.GetEventData(event.Raw.TxHash)
	if eventErr != nil {
		return fmt.Errorf("failed to get event data: %w", eventErr)
	}
	if found {
		// skip
		return nil
	}

	// get nonce
	nonce, nonceErr := c.eventHandler.GetNextNonce(event.Owner)
	if nonceErr != nil {
		return fmt.Errorf("failed to get next nonce: %w", nonceErr)
	}

	// Calculate the expected length of constructed shares based on the number of operator IDs,
	// signature length, public key length, and encrypted key length.
	operatorCount := len(event.OperatorIds)
	signatureOffset := phase0.SignatureLength
	pubKeysOffset := phase0.PublicKeyLength*operatorCount + signatureOffset
	sharesExpectedLength := encryptedKeyLength*operatorCount + pubKeysOffset

	if sharesExpectedLength != len(event.Shares) {
		err = &abiparser.MalformedEventError{
			Err: fmt.Errorf(
				"%s event shares length is not correct: expected %d, got %d",
				abiparser.ValidatorAdded,
				sharesExpectedLength,
				len(event.Shares),
			),
		}
		return err
	}

	signature := event.Shares[:signatureOffset]
	sharePublicKeys := splitBytes(event.Shares[signatureOffset:pubKeysOffset], phase0.PublicKeyLength)
	encryptedKeys := splitBytes(event.Shares[pubKeysOffset:], len(event.Shares[pubKeysOffset:])/operatorCount)

	// verify sig
	if err = verifySignature(signature, event.Owner, event.PublicKey, nonce); err != nil {
		err = &abiparser.MalformedEventError{Err: fmt.Errorf("failed to verify signature: %w", err)}
		return err
	}

	pubKey := hex.EncodeToString(event.PublicKey)

	validatorShare := c.sharesStorage.Get(event.PublicKey)
	if validatorShare == nil {
		validatorShare, err = c.handleShareCreation(c.defaultLogger, event, sharePublicKeys, encryptedKeys)
		if err != nil {
			metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusError))
			return err
		}
		valid = true
	} else if event.Owner != validatorShare.OwnerAddress {
		// Prevent multiple registration of the same validator with different owner address
		// owner A registers validator with public key X (OK)
		// owner B registers validator with public key X (NOT OK)
		err = &abiparser.MalformedEventError{
			Err: fmt.Errorf(
				"validator share already exists with different owner address: expected %s, got %s",
				validatorShare.OwnerAddress.String(),
				event.Owner.String(),
			),
		}
		return err
	}

	isOperatorShare := validatorShare.BelongsToOperator(c.operatorData.ID)
	if isOperatorShare {
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
	}

	return err
}

// TODO: refactor, consider getting rid of
func (c *controller) validatorAddedDefer(valid bool, err error, event *contract.ContractValidatorAdded) error {
	var malformedEventErr *abiparser.MalformedEventError

	if valid || errors.As(err, &malformedEventErr) {
		saveErr := c.eventHandler.SaveEventData(event.Raw.TxHash)
		if saveErr != nil {
			wrappedErr := fmt.Errorf("could not save event data: %w", saveErr)
			if err == nil {
				return wrappedErr
			}
			return errors.Join(wrappedErr, err)
		}

		bumpErr := c.eventHandler.BumpNonce(event.Owner)
		if bumpErr != nil {
			wrappedErr := fmt.Errorf("failed to bump the nonce: %w", bumpErr)
			if err == nil {
				return wrappedErr
			}
			return errors.Join(wrappedErr, err)
		}
	}

	return err
}

// onShareCreate is called when a validator was added/updated during registry sync
func (c *controller) handleShareCreation(
	logger *zap.Logger,
	validatorEvent *contract.ContractValidatorAdded,
	sharePublicKeys [][]byte,
	encryptedKeys [][]byte,
) (*ssvtypes.SSVShare, error) {
	share, shareSecret, err := validatorAddedEventToShare(
		validatorEvent,
		c.shareEncryptionKeyProvider,
		c.operatorData,
		sharePublicKeys,
		encryptedKeys,
	)
	if err != nil {
		return nil, fmt.Errorf("could not extract validator share from event: %w", err)
	}

	if share.BelongsToOperator(c.operatorData.ID) {
		if shareSecret == nil {
			return nil, errors.New("could not decode shareSecret")
		}

		logger := logger.With(fields.PubKey(share.ValidatorPubKey))

		// get metadata
		if updated, err := UpdateShareMetadata(share, c.beacon); err != nil {
			logger.Warn("could not add validator metadata", zap.Error(err))
		} else if !updated {
			logger.Warn("could not find validator metadata")
		}

		// save secret key
		if err := c.keyManager.AddShare(shareSecret); err != nil {
			return nil, fmt.Errorf("could not add share secret to key manager: %w", err)
		}
	}

	// save validator data
	if err := c.sharesStorage.Save(share); err != nil {
		return nil, fmt.Errorf("could not save validator share: %w", err)
	}

	return share, nil
}

func validatorAddedEventToShare(
	event *contract.ContractValidatorAdded,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	operatorData *registrystorage.OperatorData,
	sharePublicKeys [][]byte,
	encryptedKeys [][]byte,
) (*ssvtypes.SSVShare, *bls.SecretKey, error) {
	validatorShare := ssvtypes.SSVShare{}

	publicKey, err := ssvtypes.DeserializeBLSPublicKey(event.PublicKey)
	if err != nil {
		return nil, nil, &abiparser.MalformedEventError{
			Err: fmt.Errorf("failed to deserialize validator public key: %w", err),
		}
	}
	validatorShare.ValidatorPubKey = publicKey.Serialize()
	validatorShare.OwnerAddress = event.Owner
	var shareSecret *bls.SecretKey

	committee := make([]*spectypes.Operator, 0)
	for i := range event.OperatorIds {
		operatorID := event.OperatorIds[i]
		committee = append(committee, &spectypes.Operator{
			OperatorID: operatorID,
			PubKey:     sharePublicKeys[i],
		})
		if operatorID == operatorData.ID {
			validatorShare.OperatorID = operatorID
			validatorShare.SharePubKey = sharePublicKeys[i]

			operatorPrivateKey, found, err := shareEncryptionKeyProvider()
			if err != nil {
				return nil, nil, fmt.Errorf("could not get operator private key: %w", err)
			}
			if !found {
				return nil, nil, errors.New("could not find operator private key")
			}

			shareSecret = &bls.SecretKey{}
			decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, encryptedKeys[i])
			if err != nil {
				return nil, nil, &abiparser.MalformedEventError{
					Err: fmt.Errorf("could not decrypt share private key: %w", err),
				}
			}
			if err = shareSecret.SetHexString(string(decryptedSharePrivateKey)); err != nil {
				return nil, nil, &abiparser.MalformedEventError{
					Err: fmt.Errorf("could not set decrypted share private key: %w", err),
				}
			}
			if !bytes.Equal(shareSecret.GetPublicKey().Serialize(), validatorShare.SharePubKey) {
				return nil, nil, &abiparser.MalformedEventError{
					Err: errors.New("share private key does not match public key"),
				}
			}
		}
	}

	validatorShare.Quorum, validatorShare.PartialQuorum = ssvtypes.ComputeQuorumAndPartialQuorum(len(committee))
	validatorShare.DomainType = ssvtypes.GetDefaultDomain()
	validatorShare.Committee = committee
	validatorShare.Graffiti = []byte("ssv.network")

	return &validatorShare, shareSecret, nil
}

func (c *controller) HandleValidatorRemoved(event *contract.ContractValidatorRemoved) error {
	// TODO: handle metrics
	share := c.sharesStorage.Get(event.PublicKey)
	if share == nil {
		return &abiparser.MalformedEventError{
			Err: fmt.Errorf("could not find validator share"),
		}
	}

	// Prevent removal of the validator registered with different owner address
	// owner A registers validator with public key X (OK)
	// owner B registers validator with public key X (NOT OK)
	// owner A removes validator with public key X (OK)
	// owner B removes validator with public key X (NOT OK)
	if event.Owner != share.OwnerAddress {
		return &abiparser.MalformedEventError{
			Err: fmt.Errorf(
				"validator share already exists with different owner address: expected %s, got %s",
				share.OwnerAddress.String(),
				event.Owner.String(),
			),
		}
	}

	// remove decided messages
	messageID := spectypes.NewMsgID(ssvtypes.GetDefaultDomain(), share.ValidatorPubKey, spectypes.BNRoleAttester)
	store := c.ibftStorageMap.Get(messageID.GetRoleType())
	if store != nil {
		if err := store.CleanAllInstances(c.defaultLogger, messageID[:]); err != nil { // TODO need to delete for multi duty as well
			return fmt.Errorf("could not clean all decided messages: %w", err)
		}
	}

	// remove from storage
	if err := c.sharesStorage.Delete(share.ValidatorPubKey); err != nil {
		return fmt.Errorf("could not remove validator share: %w", err)
	}

	isOperatorShare := share.BelongsToOperator(c.operatorData.ID)
	if isOperatorShare {
		pubKey := hex.EncodeToString(event.PublicKey)
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusRemoved))
	}

	// TODO: print logs here and in other places with logFields
	logFields := make([]zap.Field, 0)
	if isOperatorShare || c.validatorOptions.FullNode {
		logFields = append(logFields,
			zap.String("validatorPubKey", hex.EncodeToString(share.ValidatorPubKey)),
			zap.String("ownerAddress", share.OwnerAddress.String()),
		)
	}

	return nil
}

func (c *controller) HandleClusterLiquidated(event *contract.ContractClusterLiquidated) ([]*ssvtypes.SSVShare, error) {
	toLiquidate, liquidatedPubKeys, err := c.processClusterEvent(c.defaultLogger, event.Owner, event.OperatorIds, true)
	if err != nil {
		return nil, fmt.Errorf("could not process cluster event: %w", err)
	}

	logFields := make([]zap.Field, 0)
	if len(liquidatedPubKeys) > 0 {
		logFields = append(logFields,
			zap.String("ownerAddress", event.Owner.String()),
			zap.Strings("liquidatedValidators", liquidatedPubKeys),
		)
	}

	return toLiquidate, nil
}

func (c *controller) HandleClusterReactivated(event *contract.ContractClusterReactivated) ([]*ssvtypes.SSVShare, error) {
	toEnable, enabledPubKeys, err := c.processClusterEvent(c.defaultLogger, event.Owner, event.OperatorIds, false)
	if err != nil {
		return nil, fmt.Errorf("could not process cluster event: %w", err)
	}

	logFields := make([]zap.Field, 0)
	if len(enabledPubKeys) > 0 {
		logFields = append(logFields,
			zap.String("ownerAddress", event.Owner.String()),
			zap.Strings("enabledValidators", enabledPubKeys),
		)
	}

	return toEnable, nil
}

func (c *controller) HandleFeeRecipientAddressUpdated(event *contract.ContractFeeRecipientAddressUpdated) error {
	recipientData := &registrystorage.RecipientData{
		Owner: event.Owner,
	}
	copy(recipientData.FeeRecipient[:], event.RecipientAddress.Bytes())

	_, err := c.recipientsStorage.SaveRecipientData(recipientData)
	if err != nil {
		return fmt.Errorf("could not save recipient data: %w", err)
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

	return nil
}
