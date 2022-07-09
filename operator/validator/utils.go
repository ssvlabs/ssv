package validator

import (
	"math/big"
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
	validatorAddedEvent abiparser.ValidatorAddedEvent,
	registryStorage registrystorage.OperatorsCollection,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	operatorPubKey string,
) (*beaconprotocol.Share, *bls.SecretKey, error) {
	validatorShare := beaconprotocol.Share{}

	// extract operator public keys from storage and fill the event
	if err := SetOperatorPublicKeys(registryStorage, &validatorAddedEvent); err != nil {
		return nil, nil, errors.Wrap(err, "could not set operator public keys")
	}

	validatorShare.PublicKey = &bls.PublicKey{}
	if err := validatorShare.PublicKey.Deserialize(validatorAddedEvent.PublicKey); err != nil {
		return nil, nil, &abiparser.MalformedEventError{
			Err: errors.Wrap(err, "failed to deserialize share public key"),
		}
	}

	validatorShare.OwnerAddress = validatorAddedEvent.OwnerAddress.String()
	var shareSecret *bls.SecretKey

	ibftCommittee := map[message.OperatorID]*beaconprotocol.Node{}
	for i := range validatorAddedEvent.OperatorPublicKeys {
		//nodeID := message.OperatorID(validatorAddedEvent.OperatorIds[i].Uint64())
		nodeID := message.OperatorID(i + 1)
		ibftCommittee[nodeID] = &beaconprotocol.Node{
			IbftID: uint64(nodeID),
			Pk:     validatorAddedEvent.SharesPublicKeys[i],
		}
		if strings.EqualFold(string(validatorAddedEvent.OperatorPublicKeys[i]), operatorPubKey) {
			validatorShare.NodeID = nodeID

			operatorPrivateKey, found, err := shareEncryptionKeyProvider()
			if err != nil {
				return nil, nil, errors.Wrap(err, "could not get operator private key")
			}
			if !found {
				return nil, nil, errors.New("could not find operator private key")
			}

			shareSecret = &bls.SecretKey{}
			decryptedSharePrivateKey, err := rsaencryption.DecodeKey(operatorPrivateKey, string(validatorAddedEvent.EncryptedKeys[i]))
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
	validatorShare.SetOperators(validatorAddedEvent.OperatorPublicKeys)
	validatorShare.SetOperatorIds(validatorAddedEvent.OperatorIds)

	return &validatorShare, shareSecret, nil
}

// SetOperatorPublicKeys extracts the operator public keys from the storage and fill the event
func SetOperatorPublicKeys(
	registryStorage registrystorage.OperatorsCollection,
	validatorAddedEvent *abiparser.ValidatorAddedEvent,
) error {
	// TODO: implement get many operators instead of just getting one by one
	// support Legacy contract
	if validatorAddedEvent.OperatorIds == nil {
		validatorAddedEvent.OperatorIds = make([]*big.Int, len(validatorAddedEvent.OperatorPublicKeys))
		for i, opPubKey := range validatorAddedEvent.OperatorPublicKeys {
			od, found, err := registryStorage.GetOperatorDataByPubKey(string(opPubKey))
			if err != nil {
				return errors.Wrap(err, "could not get operator data by public key")
			}
			if !found {
				return &abiparser.MalformedEventError{
					Err: errors.New("could not find operator data by public key"),
				}
			}
			validatorAddedEvent.OperatorIds[i] = big.NewInt(int64(od.Index))
		}
		return nil
	}
	validatorAddedEvent.OperatorPublicKeys = make([][]byte, len(validatorAddedEvent.OperatorIds))
	for i, operatorID := range validatorAddedEvent.OperatorIds {
		od, found, err := registryStorage.GetOperatorData(operatorID.Uint64())
		if err != nil {
			return errors.Wrap(err, "could not get operator's data")
		}
		if !found {
			return &abiparser.MalformedEventError{
				Err: errors.New("could not find operator data by index"),
			}
		}
		validatorAddedEvent.OperatorPublicKeys[i] = []byte(od.PublicKey)
	}
	return nil
}
