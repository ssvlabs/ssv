package validation

import (
	"bytes"
	"encoding/hex"

	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/network/commons"
)

func (mv *messageValidator) ssvMessageValidation(signedSSVMessage *spectypes.SignedSSVMessage, topic string) error {
	ssvMessage := signedSSVMessage.GetSSVMessage()

	if ssvMessage == nil {
		return ErrNilSSVMessage
	}

	//mv.metrics.SSVMessageType(ssvMessage.MsgType) // TODO

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
		return ErrTopicNotFound
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
