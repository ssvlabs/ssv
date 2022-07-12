package validator

import (
	"strings"

	"github.com/bloxapp/ssv/eth1/abiparser"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/utils/rsaencryption"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// UpdateShareMetadata will update the given share object w/o involving storage,
// it will be called only when a new share is created
func UpdateShareMetadata(share *beaconprotocol.Share, bc beaconprotocol.Beacon) (bool, error) {
	pk := share.PublicKey.SerializeToHexStr()
	results, err := beaconprotocol.FetchValidatorsMetadata(bc, [][]byte{share.PublicKey.Serialize()})
	if err != nil {
		return false, errors.Wrap(err, "failed to fetch metadata for share")
	}
	meta, ok := results[pk]
	if !ok {
		return false, nil
	}
	share.Metadata = meta
	return true, nil
}

// ShareFromValidatorEvent takes the contract event data and creates the corresponding validator share.
// share could return nil in case operator key is not present/ different
func ShareFromValidatorEvent(
	validatorRegistrationEvent abiparser.ValidatorRegistrationEvent,
	registryStorage registrystorage.OperatorsCollection,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	operatorPubKey string,
) (*beaconprotocol.Share, *bls.SecretKey, error) {
	validatorShare := beaconprotocol.Share{}

	// extract operator public keys from storage and fill the event
	if err := SetOperatorPublicKeys(registryStorage, &validatorRegistrationEvent); err != nil {
		return nil, nil, errors.Wrap(err, "could not set operator public keys")
	}

	validatorShare.PublicKey = &bls.PublicKey{}
	if err := validatorShare.PublicKey.Deserialize(validatorRegistrationEvent.PublicKey); err != nil {
		return nil, nil, &abiparser.MalformedEventError{
			Err: errors.Wrap(err, "failed to deserialize share public key"),
		}
	}

	validatorShare.OwnerAddress = validatorRegistrationEvent.OwnerAddress.String()
	var shareSecret *bls.SecretKey

	ibftCommittee := map[message.OperatorID]*beaconprotocol.Node{}
	for i := range validatorRegistrationEvent.OperatorPublicKeys {
		nodeID := message.OperatorID(i + 1)
		ibftCommittee[nodeID] = &beaconprotocol.Node{
			IbftID: uint64(nodeID),
			Pk:     validatorRegistrationEvent.SharesPublicKeys[i],
		}
		if strings.EqualFold(string(validatorRegistrationEvent.OperatorPublicKeys[i]), operatorPubKey) {
			validatorShare.NodeID = nodeID

			operatorPrivateKey, found, err := shareEncryptionKeyProvider()
			if err != nil {
				return nil, nil, errors.Wrap(err, "could not get operator private key")
			}
			if !found {
				return nil, nil, errors.New("could not find operator private key")
			}

			shareSecret = &bls.SecretKey{}
			decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, string(validatorRegistrationEvent.EncryptedKeys[i]))
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
	validatorShare.Committee = ibftCommittee
	validatorShare.SetOperators(validatorRegistrationEvent.OperatorPublicKeys)

	return &validatorShare, shareSecret, nil
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
