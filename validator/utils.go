package validator

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"strings"
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

// createShareWithOperatorKey creates a new share object from event
func createShareWithOperatorKey(validatorAddedEvent eth1.ValidatorAddedEvent, shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider) (*validatorstorage.Share, *bls.SecretKey, error) {
	operatorPrivKey, found, err := shareEncryptionKeyProvider()
	if !found {
		return nil, nil, errors.New("could not find operator private key")
	}
	if err != nil {
		return nil, nil, errors.Wrap(err, "get operator private key")
	}
	operatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not extract operator public key")
	}
	validatorShare, share, err := ShareFromValidatorAddedEvent(validatorAddedEvent, operatorPubKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not create share from event")
	}
	if share == nil {
		return nil, nil, errors.New("could not decode share key from validator added event")
	}
	if !validatorShare.OperatorReady() {
		return nil, nil, errors.New("operator share not ready")
	}
	return validatorShare, share, nil
}

// ShareFromValidatorAddedEvent takes the contract event data and creates the corresponding validator share.
// share could return nil in case operator key is not present/ different
func ShareFromValidatorAddedEvent(validatorAddedEvent eth1.ValidatorAddedEvent, operatorPubKey string) (*validatorstorage.Share, *bls.SecretKey, error) {
	validatorShare := validatorstorage.Share{}

	validatorShare.PublicKey = &bls.PublicKey{}
	if err := validatorShare.PublicKey.Deserialize(validatorAddedEvent.PublicKey); err != nil {
		return nil, nil, errors.Wrap(err, "failed to deserialize share public key")
	}
	var shareKey *bls.SecretKey

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

			shareKey = &bls.SecretKey{}
			if len(validatorAddedEvent.EncryptedKeys[i]) == 0 {
				return nil, nil, errors.New("share encrypted key invalid")
			}
			if err := shareKey.SetHexString(string(validatorAddedEvent.EncryptedKeys[i])); err != nil {
				return nil, nil, errors.Wrap(err, "failed to deserialize share private key")
			}
		}
	}
	validatorShare.Committee = ibftCommittee

	return &validatorShare, shareKey, nil
}
