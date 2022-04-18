package validator

import (
	"strings"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/ibft/proto"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// UpdateShareMetadata will update the given share object w/o involving storage,
// it will be called only when a new share is created
func UpdateShareMetadata(share *validatorstorage.Share, bc beacon.Beacon) (bool, error) {
	pk := share.PublicKey.SerializeToHexStr()
	results, err := beacon.FetchValidatorsMetadata(bc, [][]byte{share.PublicKey.Serialize()})
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
	getOperatorData registrystorage.GetOperatorData,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	operatorPubKey string,
) (*validatorstorage.Share, *bls.SecretKey, error) {
	validatorShare := validatorstorage.Share{}

	// extract operator public keys from storage and fill the event
	if err := SetOperatorPublicKeys(getOperatorData, &validatorAddedEvent); err != nil {
		return nil, nil, errors.Wrap(err, "could not extract and set operator public keys from storage")
	}

	validatorShare.PublicKey = &bls.PublicKey{}
	if err := validatorShare.PublicKey.Deserialize(validatorAddedEvent.PublicKey); err != nil {
		return nil, nil, errors.Wrap(err, "failed to deserialize share public key")
	}

	validatorShare.OwnerAddress = validatorAddedEvent.OwnerAddress.String()
	var shareSecret *bls.SecretKey

	ibftCommittee := map[uint64]*proto.Node{}
	for i := range validatorAddedEvent.OperatorPublicKeys {
		nodeID := uint64(i + 1)
		ibftCommittee[nodeID] = &proto.Node{
			IbftId: nodeID,
			Pk:     validatorAddedEvent.SharesPublicKeys[i],
		}
		if strings.EqualFold(string(validatorAddedEvent.OperatorPublicKeys[i]), operatorPubKey) {
			ibftCommittee[nodeID].Pk = validatorAddedEvent.SharesPublicKeys[i]
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
				return nil, nil, errors.Wrap(err, "failed to decrypt share private key")
			}
			decryptedSharePrivateKey = strings.Replace(decryptedSharePrivateKey, "0x", "", 1)
			if err := shareSecret.SetHexString(decryptedSharePrivateKey); err != nil {
				return nil, nil, errors.Wrap(err, "failed to set decrypted share private key")
			}
		}
	}
	validatorShare.Committee = ibftCommittee
	validatorShare.SetOperators(validatorAddedEvent.OperatorPublicKeys)

	return &validatorShare, shareSecret, nil
}

// SetOperatorPublicKeys extracts the operator public keys from the storage and fill the event
func SetOperatorPublicKeys(
	getOperatorData registrystorage.GetOperatorData,
	validatorAddedEvent *abiparser.ValidatorAddedEvent,
) error {
	validatorAddedEvent.OperatorPublicKeys = make([][]byte, len(validatorAddedEvent.OperatorIds))
	// TODO: implement get many operators instead of just getting one by one
	for i, operatorID := range validatorAddedEvent.OperatorIds {
		od, found, err := getOperatorData(operatorID.Uint64())
		if err != nil {
			return errors.Wrap(err, "could not get operator's data")
		}
		if !found {
			return errors.Wrap(err, "could not find operator's data")
		}
		validatorAddedEvent.OperatorPublicKeys[i] = []byte(od.PublicKey)
	}
	return nil
}
