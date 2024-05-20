package validation

import (
	"bytes"
	"encoding/hex"
	"fmt"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"golang.org/x/exp/slices"

	"github.com/ssvlabs/ssv/network/commons"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
)

func (mv *messageValidator) decodeSignedSSVMessage(pMsg *pubsub.Message) (*spectypes.SignedSSVMessage, error) {
	signedSSVMessage := &spectypes.SignedSSVMessage{}
	if err := signedSSVMessage.Decode(pMsg.GetData()); err != nil {
		genesisSignedSSVMessage := &genesisspectypes.SignedSSVMessage{}
		if err := genesisSignedSSVMessage.Decode(pMsg.GetData()); err == nil {
			return nil, ErrGenesisSignedSSVMessage
		}

		genesisSSVMessage := &genesisspectypes.SSVMessage{}
		if err := genesisSSVMessage.Decode(pMsg.GetData()); err == nil {
			return nil, ErrGenesisSSVMessage
		}

		e := ErrMalformedPubSubMessage
		e.innerErr = err
		return nil, e
	}

	return signedSSVMessage, nil
}

func (mv *messageValidator) validateSignedSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage) error {
	if signedSSVMessage == nil {
		return ErrNilSignedSSVMessage
	}

	signers := signedSSVMessage.GetOperatorIDs()

	if len(signers) == 0 {
		return ErrNoSigners
	}

	if !slices.IsSorted(signers) {
		return ErrSignersNotSorted
	}

	var prevSigner spectypes.OperatorID
	for _, signer := range signers {
		if signer == 0 {
			return ErrZeroSigner
		}
		// This check assumes that signers is sorted, so this rule should be after the check for ErrSignersNotSorted.
		if signer == prevSigner {
			return ErrDuplicatedSigner
		}
		prevSigner = signer
	}

	signatures := signedSSVMessage.Signatures

	if len(signatures) == 0 {
		return ErrNoSignatures
	}

	for _, signature := range signatures {
		if len(signature) == 0 {
			return ErrEmptySignature
		}

		if len(signature) != rsaSignatureSize {
			e := ErrWrongRSASignatureSize
			e.got = len(signature)
			return e
		}
	}

	if len(signers) != len(signatures) {
		e := ErrSignatureOperatorIDLengthMismatch
		e.got = fmt.Sprintf("%d/%d", len(signers), len(signatures))
		return e
	}

	ssvMessage := signedSSVMessage.SSVMessage
	if ssvMessage == nil {
		return ErrNilSSVMessage
	}

	return nil
}

func (mv *messageValidator) validateSSVMessage(ssvMessage *spectypes.SSVMessage, topic string) error {
	mv.metrics.SSVMessageType(ssvMessage.MsgType)

	if len(ssvMessage.Data) == 0 {
		return ErrEmptyData
	}

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType, spectypes.SSVPartialSignatureMsgType:
		break
	case ssvmessage.SSVEventMsgType:
		return ErrEventMessage
	case spectypes.DKGMsgType:
		return ErrDKGMessage
	default:
		e := ErrUnknownSSVMessageType
		e.got = ssvMessage.MsgType
		return e
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

	if !mv.topicMatches(ssvMessage, topic) {
		e := ErrIncorrectTopic
		e.got = topic
		return e
	}

	if len(ssvMessage.Data) > maxPayloadSize {
		err := ErrSSVDataTooBig
		err.got = len(ssvMessage.Data)
		err.want = maxPayloadSize
		return err
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

	return nil
}
