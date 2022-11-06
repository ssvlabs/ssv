package validator

import (
	"strings"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/eth1/abiparser"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/share"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// UpdateMetadataStats will update the given metadata object w/o involving storage,
// it will be called only when a new metadata is created
func UpdateMetadataStats(metadata *share.Metadata, bc beaconprotocol.Beacon) (bool, error) {
	pk := metadata.PublicKey.SerializeToHexStr()
	results, err := beaconprotocol.FetchValidatorsMetadata(bc, [][]byte{metadata.PublicKey.Serialize()})
	if err != nil {
		return false, errors.Wrap(err, "failed to fetch metadata for share")
	}
	stats, ok := results[pk]
	if !ok {
		return false, nil
	}
	metadata.Stats = stats
	return true, nil
}

// ShareFromValidatorEvent takes the contract event data and creates the corresponding validator share.
// share could return nil in case operator key is not present/ different
func ShareFromValidatorEvent(
	validatorRegistrationEvent abiparser.ValidatorRegistrationEvent,
	registryStorage registrystorage.OperatorsCollection,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	operatorPubKey string,
) (*share.Share, *share.Metadata, *bls.SecretKey, error) {
	validatorShare := share.Share{}
	shareMetadata := share.Metadata{}

	// extract operator public keys from storage and fill the event
	if err := SetOperatorPublicKeys(registryStorage, &validatorRegistrationEvent); err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not set operator public keys")
	}

	publicKey := &bls.PublicKey{}
	if err := publicKey.Deserialize(validatorRegistrationEvent.PublicKey); err != nil {
		return nil, nil, nil, &abiparser.MalformedEventError{
			Err: errors.Wrap(err, "failed to deserialize share public key"),
		}
	}
	validatorShare.ValidatorPubKey = publicKey.Serialize()
	shareMetadata.PublicKey = publicKey

	shareMetadata.OwnerAddress = validatorRegistrationEvent.OwnerAddress.String()
	var shareSecret *bls.SecretKey

	committee := make([]*spectypes.Operator, 0)
	for i := range validatorRegistrationEvent.OperatorPublicKeys {
		nodeID := spectypes.OperatorID(validatorRegistrationEvent.OperatorIds[i])
		committee = append(committee, &spectypes.Operator{
			OperatorID: nodeID,
			PubKey:     validatorRegistrationEvent.SharesPublicKeys[i],
		})
		shareMetadata.Operators = append(shareMetadata.Operators, validatorRegistrationEvent.OperatorPublicKeys[i])
		if strings.EqualFold(string(validatorRegistrationEvent.OperatorPublicKeys[i]), operatorPubKey) {
			validatorShare.OperatorID = nodeID

			operatorPrivateKey, found, err := shareEncryptionKeyProvider()
			if err != nil {
				return nil, nil, nil, errors.Wrap(err, "could not get operator private key")
			}
			if !found {
				return nil, nil, nil, errors.New("could not find operator private key")
			}

			shareSecret = &bls.SecretKey{}
			decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, string(validatorRegistrationEvent.EncryptedKeys[i]))
			if err != nil {
				return nil, nil, nil, &abiparser.MalformedEventError{
					Err: errors.Wrap(err, "failed to decrypt share private key"),
				}
			}
			decryptedSharePrivateKey = strings.Replace(decryptedSharePrivateKey, "0x", "", 1)
			if err := shareSecret.SetHexString(decryptedSharePrivateKey); err != nil {
				return nil, nil, nil, &abiparser.MalformedEventError{
					Err: errors.Wrap(err, "failed to set decrypted share private key"),
				}
			}
		}
	}
	validatorShare.Committee = committee
	for _, oid := range validatorRegistrationEvent.OperatorIds {
		shareMetadata.OperatorIDs = append(shareMetadata.OperatorIDs, uint64(oid))
	}

	return &validatorShare, &shareMetadata, shareSecret, nil
}

// SetOperatorPublicKeys extracts the operator public keys from the storage and fill the event
func SetOperatorPublicKeys(
	registryStorage registrystorage.OperatorsCollection,
	validatorRegistrationEvent *abiparser.ValidatorRegistrationEvent,
) error {
	validatorRegistrationEvent.OperatorPublicKeys = make([][]byte, len(validatorRegistrationEvent.OperatorIds))
	for i, operatorID := range validatorRegistrationEvent.OperatorIds {
		od, found, err := registryStorage.GetOperatorData(uint64(operatorID))
		if err != nil {
			return errors.Wrap(err, "could not get operator's data")
		}
		if !found {
			return &abiparser.MalformedEventError{
				Err: errors.New("could not find operator data by index"),
			}
		}
		validatorRegistrationEvent.OperatorPublicKeys[i] = []byte(od.PublicKey)
	}
	return nil
}
