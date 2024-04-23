package msgvalidation

import (
	"bytes"
	"encoding/hex"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/network/commons"
)

func (mv *messageValidator) decodeSignedSSVMessage(pMsg *pubsub.Message) (*spectypes.SignedSSVMessage, error) {
	signedSSVMessage := &spectypes.SignedSSVMessage{}
	if err := signedSSVMessage.Decode(pMsg.GetData()); err != nil {
		e := ErrMalformedPubSubMessage
		e.innerErr = err
		return nil, e
	}

	return signedSSVMessage, nil
}

func (mv *messageValidator) validateSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage, topic string) error {
	if signedSSVMessage == nil {
		return ErrEmptyPubSubMessage
	}

	operatorIDs := signedSSVMessage.GetOperatorIDs()
	signatures := signedSSVMessage.GetSignature()

	if len(operatorIDs) != len(signatures) {
		e := ErrSignatureOperatorIDLengthMismatch
		e.got = fmt.Sprintf("%d/%d", len(operatorIDs), len(signatures))
		return e
	}

	if err := mv.validateOperators(operatorIDs); err != nil {
		return err
	}

	ssvMessage := signedSSVMessage.GetSSVMessage()
	if ssvMessage == nil {
		return ErrNilSSVMessage
	}

	mv.metrics.SSVMessageType(ssvMessage.MsgType)

	if err := mv.validateSignatures(signatures); err != nil {
		return err
	}

	if len(ssvMessage.Data) == 0 {
		return ErrEmptyData
	}

	if len(ssvMessage.Data) > maxMessageSize {
		err := ErrSSVDataTooBig
		err.got = len(ssvMessage.Data)
		err.want = maxMessageSize
		return err
	}

	if !mv.topicMatches(ssvMessage, topic) {
		return ErrIncorrectTopic
	}

	if !bytes.Equal(ssvMessage.MsgID.GetDomain(), mv.netCfg.Domain[:]) {
		err := ErrWrongDomain
		err.got = hex.EncodeToString(ssvMessage.MsgID.GetDomain())
		err.want = hex.EncodeToString(mv.netCfg.Domain[:])
		return err
	}

	if !mv.validRole(ssvMessage.GetID().GetRoleType()) {
		return ErrInvalidRole
	}

	return nil
}

func (mv *messageValidator) validateSignatures(signatures [][]byte) error {
	if len(signatures) == 0 {
		return ErrNoSignatures
	}

	for _, signature := range signatures {
		if len(signature) == 0 {
			return ErrEmptySignature
		}

		if len(signature) != rsaSignatureSize {
			e := ErrWrongSignatureSize
			e.got = len(signature)
			return e
		}
	}

	return nil
}

func (mv *messageValidator) validateOperators(operatorIDs []spectypes.OperatorID) error {
	if len(operatorIDs) == 0 {
		return ErrNoSigners
	}

	if !slices.IsSorted(operatorIDs) {
		return ErrSignersNotSorted
	}

	var prevSigner spectypes.OperatorID
	for _, signer := range operatorIDs {
		if signer == 0 {
			return ErrZeroSigner
		}
		if signer == prevSigner {
			return ErrDuplicatedSigner
		}
		prevSigner = signer
	}

	return nil
}

func (mv *messageValidator) validRole(roleType spectypes.RunnerRole) bool {
	switch roleType {
	case spectypes.RoleCommittee,
		spectypes.RoleAggregator,
		spectypes.RoleProposer,
		spectypes.RoleSyncCommitteeContribution,
		spectypes.RoleValidatorRegistration,
		spectypes.RoleVoluntaryExit:
		return true
	default:
		return false
	}
}

// topicMatches checks if the message was sent on the right topic.
func (mv *messageValidator) topicMatches(ssvMessage *spectypes.SSVMessage, topic string) bool {
	getTopics := commons.ValidatorTopicID
	if mv.committeeRole(ssvMessage.GetID().GetRoleType()) {
		getTopics = commons.CommitteeTopicID
	}

	topics := getTopics(ssvMessage.GetID().GetSenderID())
	return slices.Contains(topics, commons.GetTopicBaseName(topic))
}

func (mv *messageValidator) belongsToCommittee(operatorIDs []spectypes.OperatorID, committee []spectypes.OperatorID) error {
	for _, signer := range operatorIDs {
		if !slices.Contains(committee, signer) {
			e := ErrSignerNotInCommittee
			e.got = signer
			e.want = committee
			return e
		}
	}

	if len(operatorIDs) > len(committee) {
		e := ErrTooManySigners
		e.got = len(operatorIDs)
		return e
	}

	return nil
}
