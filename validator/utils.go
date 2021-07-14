package validator

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/ibft/proto"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"strings"
)

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
