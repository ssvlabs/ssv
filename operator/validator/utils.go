package validator

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	spectypes "github.com/bloxapp/ssv-spec/types"
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
	ValidatorAddedEvent abiparser.ValidatorAddedEvent,
	operatorsCollection registrystorage.OperatorsCollection,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	operatorPubKey string,
) (*types.SSVShare, *bls.SecretKey, error) {
	validatorShare := types.SSVShare{}

	// extract operator public keys from storage and fill the event
	if err := SetOperatorPublicKeys(operatorsCollection, &ValidatorAddedEvent); err != nil {
		return nil, nil, errors.Wrap(err, "could not set operator public keys")
	}

	publicKey := &bls.PublicKey{}
	if err := publicKey.Deserialize(ValidatorAddedEvent.PublicKey); err != nil {
		return nil, nil, &abiparser.MalformedEventError{
			Err: errors.Wrap(err, "failed to deserialize share public key"),
		}
	}
	validatorShare.ValidatorPubKey = publicKey.Serialize()
	validatorShare.OwnerAddress = ValidatorAddedEvent.OwnerAddress.String()
	var shareSecret *bls.SecretKey

	committee := make([]*spectypes.Operator, 0)
	for i := range ValidatorAddedEvent.OperatorPublicKeys {
		nodeID := spectypes.OperatorID(ValidatorAddedEvent.OperatorIds[i])
		committee = append(committee, &spectypes.Operator{
			OperatorID: nodeID,
			PubKey:     ValidatorAddedEvent.SharePublicKeys[i],
		})
		if strings.EqualFold(string(ValidatorAddedEvent.OperatorPublicKeys[i]), operatorPubKey) {
			validatorShare.OperatorID = nodeID
			validatorShare.SharePubKey = ValidatorAddedEvent.SharePublicKeys[i]

			operatorPrivateKey, found, err := shareEncryptionKeyProvider()
			if err != nil {
				return nil, nil, errors.Wrap(err, "could not get operator private key")
			}
			if !found {
				return nil, nil, errors.New("could not find operator private key")
			}

			shareSecret = &bls.SecretKey{}
			decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, string(ValidatorAddedEvent.EncryptedKeys[i]))
			if err != nil {
				return nil, nil, &abiparser.MalformedEventError{
					Err: errors.Wrap(err, "failed to decrypt share private key"),
				}
			}
			decryptedSharePrivateKey = strings.Replace(decryptedSharePrivateKey, "0x", "", 1)
			if err := shareSecret.SetHexString(decryptedSharePrivateKey); err != nil {
				return nil, nil, &abiparser.MalformedEventError{
					Err: errors.Wrap(err, "failed to set decrypted share private key"),
				}
			}
		}
	}

	f := uint64(len(committee)-1) / 3
	validatorShare.Quorum = 3 * f
	validatorShare.PartialQuorum = 2 * f
	validatorShare.DomainType = types.GetDefaultDomain()
	validatorShare.Committee = committee
	validatorShare.SetOperators(ValidatorAddedEvent.OperatorPublicKeys)
	validatorShare.Graffiti = []byte("ssv.network")

	return &validatorShare, shareSecret, nil
}

// SetOperatorPublicKeys extracts the operator public keys from the storage and fill the event
func SetOperatorPublicKeys(
	operatorsCollection registrystorage.OperatorsCollection,
	ValidatorAddedEvent *abiparser.ValidatorAddedEvent,
) error {
	ValidatorAddedEvent.OperatorPublicKeys = make([][]byte, len(ValidatorAddedEvent.OperatorIds))
	for i, operatorID := range ValidatorAddedEvent.OperatorIds {
		od, found, err := operatorsCollection.GetOperatorData(operatorID)
		if err != nil {
			return errors.Wrap(err, "could not get operator's data")
		}
		if !found {
			return &abiparser.MalformedEventError{
				Err: errors.New("could not find operator data by index"),
			}
		}
		ValidatorAddedEvent.OperatorPublicKeys[i] = []byte(od.PublicKey)
	}
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
