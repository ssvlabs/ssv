package validation

import (
	"bytes"
	"encoding/hex"
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"golang.org/x/exp/slices"

	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
)

func (mv *messageValidator) decodeSignedSSVMessage(pMsg *pubsub.Message) (*spectypes.SignedSSVMessage, error) {
	// Rule: Pubsub.Message.Message.Data decoding
	signedSSVMessage := &spectypes.SignedSSVMessage{}
	if err := signedSSVMessage.Decode(pMsg.GetData()); err != nil {
		e := ErrMalformedPubSubMessage
		e.innerErr = err
		return nil, e
	}

	return signedSSVMessage, nil
}

func (mv *messageValidator) validateSignedSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage) error {
	// Rule: SignedSSVMessage cannot be nil
	if signedSSVMessage == nil {
		return ErrNilSignedSSVMessage
	}

	// Rule: Must have at least one signer
	if len(signedSSVMessage.OperatorIDs) == 0 {
		return ErrNoSigners
	}

	// Rule: Must have at least one signature
	if len(signedSSVMessage.Signatures) == 0 {
		return ErrNoSignatures
	}

	// Rule: Signature size
	for _, signature := range signedSSVMessage.Signatures {
		if len(signature) != rsaSignatureSize {
			e := ErrWrongRSASignatureSize
			e.got = len(signature)
			return e
		}
	}

	// Rule: Signers must be sorted
	if !slices.IsSorted(signedSSVMessage.OperatorIDs) {
		return ErrSignersNotSorted
	}

	var prevSigner spectypes.OperatorID
	for _, signer := range signedSSVMessage.OperatorIDs {
		// Rule: Signer can't be zero
		if signer == 0 {
			return ErrZeroSigner
		}

		// Rule: Signers must be unique
		// This check assumes that signers is sorted, so this rule should be after the check for ErrSignersNotSorted.
		if signer == prevSigner {
			return ErrDuplicatedSigner
		}
		prevSigner = signer
	}

	// Rule: Len(Signers) must be equal to Len(Signatures)
	if len(signedSSVMessage.OperatorIDs) != len(signedSSVMessage.Signatures) {
		e := ErrSignersAndSignaturesWithDifferentLength
		e.got = fmt.Sprintf("%d/%d", len(signedSSVMessage.OperatorIDs), len(signedSSVMessage.Signatures))
		return e
	}

	// Rule: SSVMessage cannot be nil
	ssvMessage := signedSSVMessage.SSVMessage
	if ssvMessage == nil {
		return ErrNilSSVMessage
	}

	return nil
}

func (mv *messageValidator) validateSSVMessage(ssvMessage *spectypes.SSVMessage) error {
	mv.metrics.SSVMessageType(ssvMessage.MsgType)

	// Rule: SSVMessage.Data must not be empty
	if len(ssvMessage.Data) == 0 {
		return ErrEmptyData
	}

	// SSVMessage.Data must respect the size limit
	if len(ssvMessage.Data) > maxPayloadDataSize {
		err := ErrSSVDataTooBig
		err.got = len(ssvMessage.Data)
		err.want = maxPayloadDataSize
		return err
	}

	switch ssvMessage.MsgType {
	case spectypes.SSVConsensusMsgType, spectypes.SSVPartialSignatureMsgType:
		break
	case ssvmessage.SSVEventMsgType:
		// Rule: Event message
		return ErrEventMessage
	case spectypes.DKGMsgType:
		// Rule: DKG message
		return ErrDKGMessage
	default:
		// Unknown message type
		e := ErrUnknownSSVMessageType
		e.got = ssvMessage.MsgType
		return e
	}

	// Rule: If domain is different then self domain
	if !bytes.Equal(ssvMessage.GetID().GetDomain(), mv.netCfg.Domain[:]) {
		err := ErrWrongDomain
		err.got = hex.EncodeToString(ssvMessage.MsgID.GetDomain())
		err.want = hex.EncodeToString(mv.netCfg.Domain[:])
		return err
	}

	// Rule: If role is invalid
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

func (mv *messageValidator) belongsToCommittee(operatorIDs []spectypes.OperatorID, committee []spectypes.OperatorID) error {
	// Rule: Signers must belong to validator committee or CommitteeID
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
