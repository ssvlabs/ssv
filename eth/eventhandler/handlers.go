package eventhandler

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/duties"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// b64 encrypted key length is 256
const encryptedKeyLength = 256

// contractParticipationDelay is the number of epochs after contract registration or reactivation
// in which the validator can start participating.
const contractParticipationDelay phase0.Epoch = 1

var (
	ErrOperatorPubkeyAlreadyExists  = fmt.Errorf("operator public key already exists")
	ErrOperatorIDAlreadyExists      = fmt.Errorf("operator ID already exists")
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

	// throw an error if operator with the same operator id already exists
	existsById, err := eh.nodeStorage.OperatorsExist(txn, []spectypes.OperatorID{event.OperatorId})
	if err != nil {
		return fmt.Errorf("could not check if operator exists: %w", err)
	}
	if existsById {
		logger.Warn("malformed event: operator ID already exists",
			fields.OperatorID(event.OperatorId))
		return &MalformedEventError{Err: ErrOperatorIDAlreadyExists}
	}

	// throw an error if there is an existing operator with the same public key
	operatorData, pubkeyExists, err := eh.nodeStorage.GetOperatorDataByPubKey(txn, event.PublicKey)
	if err != nil {
		return fmt.Errorf("could not get operator data by public key: %w", err)
	}
	if pubkeyExists {
		logger.Warn("malformed event: operator public key already exists",
			fields.OperatorPubKey(operatorData.PublicKey))
		return &MalformedEventError{Err: ErrOperatorPubkeyAlreadyExists}
	}

	exists, err := eh.nodeStorage.SaveOperatorData(txn, od)
	if err != nil {
		return fmt.Errorf("save operator data: %w", err)
	}
	if exists {
		logger.Debug("operator data already exists")
		return nil
	}

	if bytes.Equal(event.PublicKey, eh.operatorDataStore.GetOperatorData().PublicKey) {
		eh.operatorDataStore.SetOperatorData(od)
		logger = logger.With(zap.Bool("own_operator", true))
	}

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

	// Permanently remove operator data to prevent further message validation.
	if err := eh.nodeStorage.DeleteOperatorData(txn, event.OperatorId); err != nil {
		return fmt.Errorf("could not delete operator data: %w", err)
	}

	logger.Debug("processed event")
	return nil
}

func (eh *EventHandler) handleValidatorAdded(
	ctx context.Context,
	txn basedb.Txn,
	event *contract.ContractValidatorAdded,
) (ownShare *ssvtypes.SSVShare, err error) {
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

	validatorShare, exists := eh.nodeStorage.Shares().Get(txn, event.PublicKey)
	if !exists {
		shareCreated, err := eh.handleShareCreation(ctx, txn, event, sharePublicKeys, encryptedKeys)
		if err != nil {
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				logger.Warn("malformed event", zap.Error(err))

				return nil, err
			}

			return nil, err
		}

		validatorShare = shareCreated

		logger.Debug("share not found, created a new one")
	} else if event.Owner != validatorShare.OwnerAddress {
		// Prevent multiple registration of the same validator with different owner address
		// owner A registers validator with public key X (OK)
		// owner B registers validator with public key X (NOT OK)
		logger.Warn("malformed event: validator share already exists with different owner address",
			zap.String("expected", validatorShare.OwnerAddress.String()),
			zap.String("got", event.Owner.String()))

		return nil, &MalformedEventError{Err: ErrShareBelongsToDifferentOwner}
	}

	if validatorShare.BelongsToOperator(eh.operatorDataStore.GetOperatorID()) {
		ownShare = validatorShare
		logger = logger.With(zap.Bool("own_validator", true))
	}

	logger.Debug("processed event")
	return
}

// handleShareCreation is called when a validator was added/updated during registry sync
func (eh *EventHandler) handleShareCreation(
	ctx context.Context,
	txn basedb.Txn,
	validatorEvent *contract.ContractValidatorAdded,
	sharePublicKeys [][]byte,
	encryptedKeys [][]byte,
) (*ssvtypes.SSVShare, error) {
	share, encryptedKey, err := eh.validatorAddedEventToShare(
		txn,
		validatorEvent,
		sharePublicKeys,
		encryptedKeys,
	)
	if err != nil {
		return nil, fmt.Errorf("could not extract validator share from event: %w", err)
	}

	if share.BelongsToOperator(eh.operatorDataStore.GetOperatorID()) {
		if err := eh.keyManager.AddShare(ctx, encryptedKey, phase0.BLSPubKey(share.SharePubKey)); err != nil {
			var shareDecryptionEKMError ekm.ShareDecryptionError
			if errors.As(err, &shareDecryptionEKMError) {
				return nil, &MalformedEventError{Err: err}
			}
			return nil, fmt.Errorf("could not add share encrypted key: %w", err)
		}

		// Set the minimum participation epoch to match slashing protection.
		// Note: The current epoch can differ from the epoch set in slashing protection
		// due to the passage of time between saving slashing protection data and setting
		// the minimum participation epoch
		share.SetMinParticipationEpoch(eh.networkConfig.Beacon.EstimatedCurrentEpoch() + contractParticipationDelay)
	}

	// Save share to DB.
	if err := eh.nodeStorage.Shares().Save(txn, share); err != nil {
		return nil, fmt.Errorf("could not save validator share: %w", err)
	}

	return share, nil
}

func (eh *EventHandler) validatorAddedEventToShare(
	txn basedb.Txn,
	event *contract.ContractValidatorAdded,
	sharePublicKeys [][]byte,
	encryptedKeys [][]byte,
) (*ssvtypes.SSVShare, []byte, error) {
	validatorShare := ssvtypes.SSVShare{}

	publicKey, err := ssvtypes.DeserializeBLSPublicKey(event.PublicKey)
	if err != nil {
		return nil, nil, &MalformedEventError{
			Err: fmt.Errorf("failed to deserialize validator public key: %w", err),
		}
	}

	var validatorPK spectypes.ValidatorPK
	copy(validatorPK[:], publicKey.Serialize())

	validatorShare.ValidatorPubKey = validatorPK
	validatorShare.OwnerAddress = event.Owner

	selfOperatorID := eh.operatorDataStore.GetOperatorID()
	var encryptedKey []byte

	shareMembers := make([]*spectypes.ShareMember, 0)

	for i := range event.OperatorIds {
		operatorID := event.OperatorIds[i]
		_, found, err := eh.nodeStorage.GetOperatorData(txn, operatorID)
		if err != nil {
			return nil, nil, fmt.Errorf("could not get operator data: %w", err)
		}
		if !found {
			return nil, nil, &MalformedEventError{
				Err: fmt.Errorf("operator data not found: %w", err),
			}
		}

		shareMembers = append(shareMembers, &spectypes.ShareMember{
			Signer:      operatorID,
			SharePubKey: sharePublicKeys[i],
		})

		if operatorID != selfOperatorID {
			continue
		}

		//validatorShare.OperatorID = operatorID
		validatorShare.SharePubKey = sharePublicKeys[i]
		encryptedKey = encryptedKeys[i]
	}

	validatorShare.DomainType = eh.networkConfig.DomainType
	validatorShare.Committee = shareMembers

	return &validatorShare, encryptedKey, nil
}

var emptyPK = [48]byte{}

func (eh *EventHandler) handleValidatorRemoved(ctx context.Context, txn basedb.Txn, event *contract.ContractValidatorRemoved) (spectypes.ValidatorPK, error) {
	logger := eh.logger.With(
		fields.EventName(ValidatorRemoved),
		fields.TxHash(event.Raw.TxHash),
		fields.Owner(event.Owner),
		fields.OperatorIDs(event.OperatorIds),
		fields.PubKey(event.PublicKey),
	)
	logger.Debug("processing event")

	// TODO: handle metrics
	share, exists := eh.nodeStorage.Shares().Get(txn, event.PublicKey)
	if !exists {
		logger.Warn("malformed event: could not find validator share")
		return emptyPK, &MalformedEventError{Err: ErrValidatorShareNotFound}
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

		return emptyPK, &MalformedEventError{Err: ErrShareBelongsToDifferentOwner}
	}

	if err := eh.nodeStorage.Shares().Delete(txn, share.ValidatorPubKey[:]); err != nil {
		return emptyPK, fmt.Errorf("could not remove validator share: %w", err)
	}

	isOperatorShare := share.BelongsToOperator(eh.operatorDataStore.GetOperatorID())
	if isOperatorShare || eh.fullNode {
		logger = logger.With(zap.String("validator_pubkey", hex.EncodeToString(share.ValidatorPubKey[:])))
	}
	if isOperatorShare {
		err := eh.keyManager.RemoveShare(ctx, phase0.BLSPubKey(share.SharePubKey))
		if err != nil {
			return emptyPK, fmt.Errorf("could not remove share from ekm storage: %w", err)
		}

		// Remove validator from doppelganger service
		eh.doppelgangerHandler.RemoveValidatorState(share.ValidatorIndex)

		logger.Debug("processed event")
		return share.ValidatorPubKey, nil
	}

	logger.Debug("processed event")
	return emptyPK, nil
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

	// Remove validator shares from doppelganger service
	for _, share := range toLiquidate {
		eh.doppelgangerHandler.RemoveValidatorState(share.ValidatorIndex)
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
		if err := eh.keyManager.BumpSlashingProtection(phase0.BLSPubKey(share.SharePubKey)); err != nil {
			return nil, fmt.Errorf("could not bump slashing protection: %w", err)
		}

		// Set the minimum participation epoch to match slashing protection.
		// Note: The current epoch can differ from the epoch set in slashing protection
		// due to the passage of time between saving slashing protection data and setting
		// the minimum participation epoch
		share.SetMinParticipationEpoch(eh.networkConfig.Beacon.EstimatedCurrentEpoch() + contractParticipationDelay)
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

func (eh *EventHandler) handleValidatorExited(txn basedb.Txn, event *contract.ContractValidatorExited) (*duties.ExitDescriptor, error) {
	logger := eh.logger.With(
		fields.EventName(ValidatorExited),
		fields.TxHash(event.Raw.TxHash),
		fields.PubKey(event.PublicKey),
		fields.OperatorIDs(event.OperatorIds),
	)
	logger.Debug("processing event")
	defer logger.Debug("processed event")

	share, exists := eh.nodeStorage.Shares().Get(txn, event.PublicKey)
	if !exists {
		logger.Warn("malformed event: could not find validator share")
		return nil, &MalformedEventError{Err: ErrValidatorShareNotFound}
	}

	if event.Owner != share.OwnerAddress {
		logger.Warn("malformed event: validator share already exists with different owner address",
			zap.String("expected", share.OwnerAddress.String()),
			zap.String("got", event.Owner.String()))

		return nil, &MalformedEventError{Err: ErrShareBelongsToDifferentOwner}
	}

	if !share.HasBeaconMetadata() {
		return nil, nil
	}

	pk := phase0.BLSPubKey{}
	copy(pk[:], share.ValidatorPubKey[:])

	ed := &duties.ExitDescriptor{
		OwnValidator:   false,
		PubKey:         pk,
		ValidatorIndex: share.ValidatorIndex,
		BlockNumber:    event.Raw.BlockNumber,
	}
	if share.BelongsToOperator(eh.operatorDataStore.GetOperatorID()) {
		ed.OwnValidator = true
	}

	return ed, nil
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
	clusterID := ssvtypes.ComputeClusterIDHash(owner, operatorIDs)
	shares := eh.nodeStorage.Shares().List(txn, registrystorage.ByClusterIDHash(clusterID))
	toUpdate := make([]*ssvtypes.SSVShare, 0, len(shares))
	var operatorShares []*ssvtypes.SSVShare
	var operatorValidatorPubKeys []string

	for _, share := range shares {
		share.Liquidated = toLiquidate
		toUpdate = append(toUpdate, share)

		if isOperatorShare := share.BelongsToOperator(eh.operatorDataStore.GetOperatorData().ID); isOperatorShare {
			operatorShares = append(operatorShares, share)
			operatorValidatorPubKeys = append(operatorValidatorPubKeys, hex.EncodeToString(share.ValidatorPubKey[:]))
		}
	}

	if len(toUpdate) > 0 {
		if err := eh.nodeStorage.Shares().Save(txn, toUpdate...); err != nil {
			return nil, nil, fmt.Errorf("could not save validator shares: %w", err)
		}
	}

	return operatorShares, operatorValidatorPubKeys, nil
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
