package eventhandler

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/logging/fields"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v2/types"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// b64 encrypted key length is 256
const encryptedKeyLength = 256

var (
	ErrAlreadyRegistered            = fmt.Errorf("operator registered with the same operator public key")
	ErrOperatorDataNotFound         = fmt.Errorf("operator data not found")
	ErrIncorrectSharesLength        = fmt.Errorf("shares length is not correct")
	ErrSignatureVerification        = fmt.Errorf("signature verification failed")
	ErrShareBelongsToDifferentOwner = fmt.Errorf("share already exists and belongs to different owner")
	ErrValidatorShareNotFound       = fmt.Errorf("validator share not found")
)

// TODO: make sure all handlers are tested properly:
// set up a mock DB where we test that after running the handler we check that the DB state is as expected

func (eh *EventHandler) handleOperatorAdded(txn basedb.Txn, event *contract.ContractOperatorAdded) error {
	logger := eh.logger.With(
		fields.EventName(OperatorAdded),
		fields.TxHash(event.Raw.TxHash),
		fields.OperatorID(event.OperatorId),
		fields.Owner(event.Owner),
		fields.OperatorPubKey(event.PublicKey),
	)
	logger.Debug("processing event")

	od := &registrystorage.OperatorData{
		PublicKey:    event.PublicKey,
		OwnerAddress: event.Owner,
		ID:           event.OperatorId,
	}

	// throw an error if there is an existing operator with the same public key and different operator id
	operatorData := eh.operatorData.GetOperatorData()
	if operatorData.ID != 0 && bytes.Equal(operatorData.PublicKey, event.PublicKey) && operatorData.ID != event.OperatorId {
		logger.Warn("malformed event: operator registered with the same operator public key",
			zap.Uint64("expected_operator_id", operatorData.ID))
		return &MalformedEventError{Err: ErrAlreadyRegistered}
	}

	// TODO: consider saving other operators as well
	exists, err := eh.nodeStorage.SaveOperatorData(txn, od)
	if err != nil {
		return fmt.Errorf("save operator data: %w", err)
	}
	if exists {
		logger.Debug("operator data already exists")
		return nil
	}

	ownOperator := bytes.Equal(event.PublicKey, operatorData.PublicKey)
	if ownOperator {
		eh.operatorData.SetOperatorData(od)
		logger = logger.With(zap.Bool("own_operator", ownOperator))
	}

	eh.metrics.OperatorPublicKey(od.ID, od.PublicKey)
	logger.Debug("processed event")

	return nil
}

func (eh *EventHandler) handleOperatorRemoved(txn basedb.Txn, event *contract.ContractOperatorRemoved) error {
	logger := eh.logger.With(
		fields.EventName(OperatorRemoved),
		fields.TxHash(event.Raw.TxHash),
		fields.OperatorID(event.OperatorId),
	)
	logger.Debug("processing event")

	od, found, err := eh.nodeStorage.GetOperatorData(txn, event.OperatorId)
	if err != nil {
		return fmt.Errorf("could not get operator data: %w", err)
	}
	if !found || od == nil {
		logger.Warn("malformed event: could not find operator data")
		return &MalformedEventError{Err: ErrOperatorDataNotFound}
	}

	logger = logger.With(
		fields.OperatorPubKey(od.PublicKey),
		fields.Owner(od.OwnerAddress),
	)

	// TODO: In original handler we didn't delete operator data, so this behavior was preserved. However we likely need to.
	// TODO: Delete operator from all the shares.
	//	var shares []Share
	//	for _, s := range nodeStorage.Shares().List() {
	//		// if operator in committee, delete him from it:
	//		//     shares = append(shares, s)
	//	}
	//	nodeStorage.Shares().Save(shares)
	// err = eh.nodeStorage.DeleteOperatorData(txn, od.ID)
	// if err != nil {
	// 	return err
	// }

	logger.Debug("processed event")
	return nil
}

func (eh *EventHandler) handleValidatorAdded(txn basedb.Txn, event *contract.ContractValidatorAdded) (ownShare *ssvtypes.SSVShare, err error) {
	logger := eh.logger.With(
		fields.EventName(ValidatorAdded),
		fields.TxHash(event.Raw.TxHash),
		fields.Owner(event.Owner),
		fields.OperatorIDs(event.OperatorIds),
		fields.Validator(event.PublicKey),
	)

	// Get the expected nonce.
	nonce, nonceErr := eh.nodeStorage.GetNextNonce(txn, event.Owner)
	if nonceErr != nil {
		return nil, fmt.Errorf("failed to get next nonce: %w", nonceErr)
	}

	// Bump nonce. This transaction would be reverted later if the handling fails,
	// unless the failure is due to a malformed event.
	if err := eh.nodeStorage.BumpNonce(txn, event.Owner); err != nil {
		return nil, err
	}

	if err := eh.validateOperators(txn, event.OperatorIds); err != nil {
		return nil, &MalformedEventError{Err: err}
	}

	// Calculate the expected length of constructed shares based on the number of operator IDs,
	// signature length, public key length, and encrypted key length.
	operatorCount := len(event.OperatorIds)
	signatureOffset := phase0.SignatureLength
	pubKeysOffset := phase0.PublicKeyLength*operatorCount + signatureOffset
	sharesExpectedLength := encryptedKeyLength*operatorCount + pubKeysOffset

	if sharesExpectedLength != len(event.Shares) {
		logger.Warn("malformed event: event shares length is not correct",
			zap.Int("expected", sharesExpectedLength),
			zap.Int("got", len(event.Shares)))

		return nil, &MalformedEventError{Err: ErrIncorrectSharesLength}
	}

	signature := event.Shares[:signatureOffset]
	sharePublicKeys := splitBytes(event.Shares[signatureOffset:pubKeysOffset], phase0.PublicKeyLength)
	encryptedKeys := splitBytes(event.Shares[pubKeysOffset:], len(event.Shares[pubKeysOffset:])/operatorCount)

	// verify sig
	if err := verifySignature(signature, event.Owner, event.PublicKey, nonce); err != nil {
		logger.Warn("malformed event: failed to verify signature",
			zap.String("signature", hex.EncodeToString(signature)),
			zap.String("owner", event.Owner.String()),
			zap.String("validator_public_key", hex.EncodeToString(event.PublicKey)),
			zap.Error(err))

		return nil, &MalformedEventError{Err: ErrSignatureVerification}
	}

	validatorShare := eh.nodeStorage.Shares().Get(txn, event.PublicKey)

	if validatorShare == nil {
		createdShare, err := eh.handleShareCreation(txn, event, sharePublicKeys, encryptedKeys)
		if err != nil {
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				logger.Warn("malformed event", zap.Error(err))

				return nil, err
			}

			eh.metrics.ValidatorError(event.PublicKey)
			return nil, err
		}

		validatorShare = createdShare

		logger.Debug("share not found, created a new one", fields.OperatorID(validatorShare.OperatorID))
	} else if event.Owner != validatorShare.OwnerAddress {
		// Prevent multiple registration of the same validator with different owner address
		// owner A registers validator with public key X (OK)
		// owner B registers validator with public key X (NOT OK)
		logger.Warn("malformed event: validator share already exists with different owner address",
			zap.String("expected", validatorShare.OwnerAddress.String()),
			zap.String("got", event.Owner.String()))

		return nil, &MalformedEventError{Err: ErrShareBelongsToDifferentOwner}
	}

	isOperatorShare := validatorShare.BelongsToOperator(eh.operatorData.GetOperatorData().ID)
	if isOperatorShare {
		eh.metrics.ValidatorInactive(event.PublicKey)
		ownShare = validatorShare
		logger = logger.With(zap.Bool("own_validator", isOperatorShare))
	}

	logger.Debug("processed event")
	return
}

// handleShareCreation is called when a validator was added/updated during registry sync
func (eh *EventHandler) handleShareCreation(
	txn basedb.Txn,
	validatorEvent *contract.ContractValidatorAdded,
	sharePublicKeys [][]byte,
	encryptedKeys [][]byte,
) (*ssvtypes.SSVShare, error) {
	share, shareSecret, err := validatorAddedEventToShare(
		validatorEvent,
		eh.shareEncryptionKeyProvider,
		eh.operatorData.GetOperatorData(),
		sharePublicKeys,
		encryptedKeys,
	)
	if err != nil {
		return nil, fmt.Errorf("could not extract validator share from event: %w", err)
	}

	if share.BelongsToOperator(eh.operatorData.GetOperatorData().ID) {
		if shareSecret == nil {
			return nil, errors.New("could not decode shareSecret")
		}

		// Save secret key into KeyManager.
		if err := eh.keyManager.AddShare(shareSecret); err != nil {
			return nil, fmt.Errorf("could not add share secret to key manager: %w", err)
		}
	}

	// Save share.
	if err := eh.nodeStorage.Shares().Save(txn, share); err != nil {
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
		return nil, nil, &MalformedEventError{
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

		if operatorID != operatorData.ID {
			continue
		}

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
			return nil, nil, &MalformedEventError{
				Err: fmt.Errorf("could not decrypt share private key: %w", err),
			}
		}
		if err = shareSecret.SetHexString(string(decryptedSharePrivateKey)); err != nil {
			return nil, nil, &MalformedEventError{
				Err: fmt.Errorf("could not set decrypted share private key: %w", err),
			}
		}
		if !bytes.Equal(shareSecret.GetPublicKey().Serialize(), validatorShare.SharePubKey) {
			return nil, nil, &MalformedEventError{
				Err: errors.New("share private key does not match public key"),
			}
		}
	}

	validatorShare.Quorum, validatorShare.PartialQuorum = ssvtypes.ComputeQuorumAndPartialQuorum(len(committee))
	validatorShare.DomainType = ssvtypes.GetDefaultDomain()
	validatorShare.Committee = committee
	validatorShare.Graffiti = []byte("ssv.network")

	return &validatorShare, shareSecret, nil
}

func (eh *EventHandler) handleValidatorRemoved(txn basedb.Txn, event *contract.ContractValidatorRemoved) (spectypes.ValidatorPK, error) {
	logger := eh.logger.With(
		fields.EventName(ValidatorRemoved),
		fields.TxHash(event.Raw.TxHash),
		fields.Owner(event.Owner),
		fields.OperatorIDs(event.OperatorIds),
		fields.PubKey(event.PublicKey),
	)
	logger.Debug("processing event")

	// TODO: handle metrics
	share := eh.nodeStorage.Shares().Get(txn, event.PublicKey)
	if share == nil {
		logger.Warn("malformed event: could not find validator share")
		return nil, &MalformedEventError{Err: ErrValidatorShareNotFound}
	}

	// Prevent removal of the validator registered with different owner address
	// owner A registers validator with public key X (OK)
	// owner B registers validator with public key X (NOT OK)
	// owner A removes validator with public key X (OK)
	// owner B removes validator with public key X (NOT OK)
	if event.Owner != share.OwnerAddress {
		logger.Warn("malformed event: validator share already exists with different owner address",
			zap.String("expected", share.OwnerAddress.String()),
			zap.String("got", event.Owner.String()))

		return nil, &MalformedEventError{Err: ErrShareBelongsToDifferentOwner}
	}

	removeDecidedMessages := func(role spectypes.BeaconRole, store qbftstorage.QBFTStore) error {
		messageID := spectypes.NewMsgID(types.GetDefaultDomain(), share.ValidatorPubKey, role)
		return store.CleanAllInstances(logger, messageID[:])
	}
	err := eh.storageMap.Each(removeDecidedMessages)
	if err != nil {
		return nil, fmt.Errorf("could not clean all decided messages: %w", err)
	}

	if err := eh.nodeStorage.Shares().Delete(txn, share.ValidatorPubKey); err != nil {
		return nil, fmt.Errorf("could not remove validator share: %w", err)
	}

	isOperatorShare := share.BelongsToOperator(eh.operatorData.GetOperatorData().ID)
	if isOperatorShare || eh.fullNode {
		logger = logger.With(zap.String("validator_pubkey", hex.EncodeToString(share.ValidatorPubKey)))
	}
	if isOperatorShare {
		err = eh.keyManager.RemoveShare(hex.EncodeToString(share.SharePubKey))
		if err != nil {
			return nil, fmt.Errorf("could not remove share from ekm storage: %w", err)
		}

		eh.metrics.ValidatorRemoved(event.PublicKey)
		logger.Debug("processed event")
		return share.ValidatorPubKey, nil
	}

	logger.Debug("processed event")
	return nil, nil
}

func (eh *EventHandler) handleClusterLiquidated(txn basedb.Txn, event *contract.ContractClusterLiquidated) ([]*ssvtypes.SSVShare, error) {
	logger := eh.logger.With(
		fields.EventName(ClusterLiquidated),
		fields.TxHash(event.Raw.TxHash),
		fields.Owner(event.Owner),
		fields.OperatorIDs(event.OperatorIds),
	)
	logger.Debug("processing event")

	toLiquidate, liquidatedPubKeys, err := eh.processClusterEvent(txn, event.Owner, event.OperatorIds, true)
	if err != nil {
		return nil, fmt.Errorf("could not process cluster event: %w", err)
	}

	if len(liquidatedPubKeys) > 0 {
		logger = logger.With(zap.Strings("liquidated_validators", liquidatedPubKeys))
	}

	logger.Debug("processed event")
	return toLiquidate, nil
}

func (eh *EventHandler) handleClusterReactivated(txn basedb.Txn, event *contract.ContractClusterReactivated) ([]*ssvtypes.SSVShare, error) {
	logger := eh.logger.With(
		fields.EventName(ClusterReactivated),
		fields.TxHash(event.Raw.TxHash),
		fields.Owner(event.Owner),
		fields.OperatorIDs(event.OperatorIds),
	)
	logger.Debug("processing event")

	toReactivate, enabledPubKeys, err := eh.processClusterEvent(txn, event.Owner, event.OperatorIds, false)
	if err != nil {
		return nil, fmt.Errorf("could not process cluster event: %w", err)
	}

	// bump slashing protection for operator reactivated validators
	for _, share := range toReactivate {
		if err := eh.keyManager.(ekm.StorageProvider).BumpSlashingProtection(share.SharePubKey); err != nil {
			return nil, fmt.Errorf("could not bump slashing protection: %w", err)
		}
	}

	if len(enabledPubKeys) > 0 {
		logger = logger.With(zap.Strings("enabled_validators", enabledPubKeys))
	}

	logger.Debug("processed event")
	return toReactivate, nil
}

func (eh *EventHandler) handleFeeRecipientAddressUpdated(txn basedb.Txn, event *contract.ContractFeeRecipientAddressUpdated) (bool, error) {
	logger := eh.logger.With(
		fields.EventName(FeeRecipientAddressUpdated),
		fields.TxHash(event.Raw.TxHash),
		fields.Owner(event.Owner),
		fields.FeeRecipient(event.RecipientAddress.Bytes()),
	)
	logger.Debug("processing event")

	recipientData, found, err := eh.nodeStorage.GetRecipientData(txn, event.Owner)
	if err != nil {
		return false, fmt.Errorf("get recipient data: %w", err)
	}

	if !found || recipientData == nil {
		recipientData = &registrystorage.RecipientData{
			Owner: event.Owner,
		}
	}

	copy(recipientData.FeeRecipient[:], event.RecipientAddress.Bytes())

	r, err := eh.nodeStorage.SaveRecipientData(txn, recipientData)
	if err != nil {
		return false, fmt.Errorf("could not save recipient data: %w", err)
	}

	logger.Debug("processed event")
	return r != nil, nil
}

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

// processClusterEvent handles registry contract event for cluster
func (eh *EventHandler) processClusterEvent(
	txn basedb.Txn,
	owner ethcommon.Address,
	operatorIDs []uint64,
	toLiquidate bool,
) ([]*ssvtypes.SSVShare, []string, error) {
	clusterID, err := ssvtypes.ComputeClusterIDHash(owner.Bytes(), operatorIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("could not compute share cluster id: %w", err)
	}

	shares := eh.nodeStorage.Shares().List(txn, registrystorage.ByClusterID(clusterID))
	toUpdate := make([]*ssvtypes.SSVShare, 0)
	updatedPubKeys := make([]string, 0)

	for _, share := range shares {
		isOperatorShare := share.BelongsToOperator(eh.operatorData.GetOperatorData().ID)
		if isOperatorShare || eh.fullNode {
			updatedPubKeys = append(updatedPubKeys, hex.EncodeToString(share.ValidatorPubKey))
		}
		if isOperatorShare {
			share.Liquidated = toLiquidate
			toUpdate = append(toUpdate, share)
		}
	}

	if len(toUpdate) > 0 {
		if err = eh.nodeStorage.Shares().Save(txn, toUpdate...); err != nil {
			return nil, nil, fmt.Errorf("could not save validator shares: %w", err)
		}
	}

	return toUpdate, updatedPubKeys, nil
}

// MalformedEventError is returned when event is malformed
type MalformedEventError struct {
	Err error
}

func (e *MalformedEventError) Error() string {
	return e.Err.Error()
}

func (e *MalformedEventError) Unwrap() error {
	return e.Err
}
