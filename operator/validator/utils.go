package validator

import (
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/ibft/proto"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	validatorstorage "github.com/bloxapp/ssv/protocol/v1/validator/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"strings"
)

// UpdateShareMetadata will update the given share object w/o involving storage,
// it will be called only when a new share is created
func UpdateShareMetadata(share *validatorstorage.Share, bc beaconprotocol.Beacon) (bool, error) {
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

// createShareWithOperatorKey creates a new share object from event
func createShareWithOperatorKey(
	validatorAddedEvent abiparser.ValidatorAddedEvent,
	operatorPubKey string,
	isOperatorShare bool,
) (*validatorstorage.Share, *bls.SecretKey, error) {
	validatorShare, shareSecret, err := ShareFromValidatorAddedEvent(validatorAddedEvent, operatorPubKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not extract validator share from event")
	}

	// handle the case where the validator share belongs to operator
	if isOperatorShare {
		if shareSecret == nil {
			return nil, nil, errors.New("could not decode shareSecret key from ValidatorAdded event")
		}
		if !validatorShare.OperatorReady() {
			return nil, nil, errors.New("operator validator share not ready")
		}
	}
	return validatorShare, shareSecret, nil
}

// ShareFromValidatorAddedEvent takes the contract event data and creates the corresponding validator share.
// share could return nil in case operator key is not present/ different
func ShareFromValidatorAddedEvent(
	validatorAddedEvent abiparser.ValidatorAddedEvent,
	operatorPubKey string,
) (*validatorstorage.Share, *bls.SecretKey, error) {
	validatorShare := validatorstorage.Share{}

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

			shareSecret = &bls.SecretKey{}
			if len(validatorAddedEvent.EncryptedKeys[i]) == 0 {
				return nil, nil, errors.New("share encrypted key invalid")
			}
			if err := shareSecret.SetHexString(string(validatorAddedEvent.EncryptedKeys[i])); err != nil {
				return nil, nil, errors.Wrap(err, "failed to deserialize share private key")
			}
		}
	}
	validatorShare.Committee = ibftCommittee
	validatorShare.SetOperators(validatorAddedEvent.OperatorPublicKeys)

	return &validatorShare, shareSecret, nil
}
