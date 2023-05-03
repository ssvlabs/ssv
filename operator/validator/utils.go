package validator

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// UpdateShareMetadata will update the given share object w/o involving storage,
// it will be called only when a new share is created
func UpdateShareMetadata(share *types.SSVShare, bc beaconprotocol.Beacon) (bool, error) {
	pk := hex.EncodeToString(share.ValidatorPubKey)
	results, err := beaconprotocol.FetchValidatorsMetadata(bc, [][]byte{share.ValidatorPubKey})
	if err != nil {
		return false, errors.Wrap(err, "failed to fetch metadata for share")
	}
	meta, ok := results[pk]
	if !ok {
		return false, nil
	}
	share.BeaconMetadata = meta
	return true, nil
}

// ShareFromValidatorEvent takes the contract event data and creates the corresponding validator share.
// share could return nil in case operator key is not present/ different
func ShareFromValidatorEvent(
	event abiparser.ValidatorAddedEvent,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	operatorData *registrystorage.OperatorData,
) (*types.SSVShare, *bls.SecretKey, error) {
	validatorShare := types.SSVShare{}

	publicKey, err := types.DeserializeBLSPublicKey(event.PublicKey)
	if err != nil {
		return nil, nil, &abiparser.MalformedEventError{
			Err: errors.Wrap(err, "failed to deserialize validator public key"),
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
			PubKey:     event.SharePublicKeys[i],
		})
		if operatorID == operatorData.ID {
			validatorShare.OperatorID = operatorID
			validatorShare.SharePubKey = event.SharePublicKeys[i]

			operatorPrivateKey, found, err := shareEncryptionKeyProvider()
			if err != nil {
				return nil, nil, errors.Wrap(err, "could not get operator private key")
			}
			if !found {
				return nil, nil, errors.New("could not find operator private key")
			}

			shareSecret = &bls.SecretKey{}
			decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, event.EncryptedKeys[i])
			if err != nil {
				return nil, nil, &abiparser.MalformedEventError{
					Err: errors.Wrap(err, "could not decrypt share private key"),
				}
			}
			if err = shareSecret.SetHexString(string(decryptedSharePrivateKey)); err != nil {
				return nil, nil, &abiparser.MalformedEventError{
					Err: errors.Wrap(err, "could not set decrypted share private key"),
				}
			}
			if !bytes.Equal(shareSecret.GetPublicKey().Serialize(), validatorShare.SharePubKey) {
				return nil, nil, &abiparser.MalformedEventError{
					Err: errors.New("share private key does not match public key"),
				}
			}
		}
	}

	validatorShare.DomainType = types.GetDefaultDomain()
	validatorShare.Committee = committee
	validatorShare.Graffiti = []byte("ssv.network")

	return &validatorShare, shareSecret, nil
}

func SetShareFeeRecipient(share *types.SSVShare, getRecipientData GetRecipientDataFunc) error {
	var feeRecipient bellatrix.ExecutionAddress
	data, found, err := getRecipientData(share.OwnerAddress)
	if err != nil {
		return errors.Wrap(err, "could not get recipient data")
	}
	if !found {
		copy(feeRecipient[:], share.OwnerAddress.Bytes())
	} else {
		feeRecipient = data.FeeRecipient
	}
	share.SetFeeRecipient(feeRecipient)

	return nil
}

func LoadLocalEvents(logger *zap.Logger, handler eth1.SyncEventHandler, path string) error {
	yamlFile, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}

	var parsedData []*eth1.Event
	err = yaml.Unmarshal(yamlFile, &parsedData)
	if err != nil {
		return err
	}
	for _, ev := range parsedData {
		logFields, err := handler(*ev)
		errs := eth1.HandleEventResult(logger, *ev, logFields, err, false)
		if len(errs) > 0 {
			logger.Warn("could not handle some of the events during local events sync", zap.Any("errs", errs))
			return errors.New("could not handle some of the events during local events sync")
		}
	}

	logger.Info("managed to sync local events")
	return nil
}
