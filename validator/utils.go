package validator

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"strings"
)

// updateShareMetadata will update the given share object w/o involving storage,
// it will be called only when a new share is created
func updateShareMetadata(share *validatorstorage.Share, bc beacon.Beacon) (bool, error) {
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
func createShareWithOperatorKey(validatorAddedEvent eth1.ValidatorAddedEvent, shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider) (*validatorstorage.Share, error) {
	operatorPrivKey, found, err := shareEncryptionKeyProvider()
	if !found {
		return nil, errors.New("could not find operator private key")
	}
	if err != nil {
		return nil, errors.Wrap(err, "get operator private key")
	}
	operatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not extract operator public key")
	}
	validatorShare, err := ShareFromValidatorAddedEvent(validatorAddedEvent, operatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not create share from event")
	}
	return validatorShare, nil
}

// ShareFromValidatorAddedEvent takes the contract event data and creates the corresponding validator share
func ShareFromValidatorAddedEvent(validatorAddedEvent eth1.ValidatorAddedEvent, operatorPubKey string) (*validatorstorage.Share, error) {
	validatorShare := validatorstorage.Share{}

	validatorShare.PublicKey = &bls.PublicKey{}
	if err := validatorShare.PublicKey.Deserialize(validatorAddedEvent.PublicKey); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize share public key")
	}
	validatorShare.ShareKey = &bls.SecretKey{}

	ibftCommittee := map[uint64]*proto.Node{}
	for i := range validatorAddedEvent.OessList {
		oess := validatorAddedEvent.OessList[i]
		nodeID := oess.Index.Uint64() + 1
		ibftCommittee[nodeID] = &proto.Node{
			IbftId: nodeID,
			Pk:     oess.SharedPublicKey,
		}
		if strings.EqualFold(string(oess.OperatorPublicKey), operatorPubKey) {
			ibftCommittee[nodeID].Pk = oess.SharedPublicKey
			validatorShare.NodeID = nodeID

			if err := validatorShare.ShareKey.SetHexString(string(oess.EncryptedKey)); err != nil {
				return nil, errors.Wrap(err, "failed to deserialize share private key")
			}
			ibftCommittee[nodeID].Sk = validatorShare.ShareKey.Serialize()
		}
	}
	validatorShare.Committee = ibftCommittee

	return &validatorShare, nil
}

// IdentifierFormat return base format for lambda
func IdentifierFormat(pubKey []byte, role beacon.RoleType) string {
	return fmt.Sprintf("%s_%s", hex.EncodeToString(pubKey), role.String())
}
