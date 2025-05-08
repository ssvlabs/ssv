package validation

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"slices"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	spectypes "github.com/ssvlabs/ssv-spec/types"

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

	// Rule: Len(Signers) must be equal to Len(Signatures)
	if len(signedSSVMessage.OperatorIDs) != len(signedSSVMessage.Signatures) {
		e := ErrSignersAndSignaturesWithDifferentLength
		e.got = fmt.Sprintf("%d/%d", len(signedSSVMessage.OperatorIDs), len(signedSSVMessage.Signatures))
		return e
	}

	var prevSigner spectypes.OperatorID

	for _, signer := range signedSSVMessage.OperatorIDs {
		// Rule: Signer can't be zero
		if err := mv.validateSignerNotZero(signer); err != nil {
			return err
		}

		// Rule: Signers must be unique
		// This check assumes that signers is sorted, so this rule should be after the check for ErrSignersNotSorted.
		if err := mv.validateSignerUnique(signer, prevSigner); err != nil {
			return err
		}

		// Rule: Signer must exist
		if err := mv.validateSignerIsKnown(signer); err != nil {
			return err
		}

		prevSigner = signer
	}

	// Rule: SSVMessage cannot be nil
	ssvMessage := signedSSVMessage.SSVMessage
	if ssvMessage == nil {
		return ErrNilSSVMessage
	}

	return nil
}

func (mv *messageValidator) validateSSVMessage(ssvMessage *spectypes.SSVMessage) error {
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
	default:
		// Unknown message type
		e := ErrUnknownSSVMessageType
		e.got = ssvMessage.MsgType
		return e
	}

	// Rule: If domain is different then self domain
	domain := mv.netCfg.DomainType
	if !bytes.Equal(ssvMessage.GetID().GetDomain(), domain[:]) {
		err := ErrWrongDomain
		err.got = hex.EncodeToString(ssvMessage.MsgID.GetDomain())
		err.want = hex.EncodeToString(domain[:])
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

// belongsToCommittee checks if the signers belong to the committee.
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

// validateSignerNotZero checks if the signer ID is not zero.
func (mv *messageValidator) validateSignerNotZero(signer spectypes.OperatorID) error {
	if signer == 0 {
		return ErrZeroSigner
	}
	return nil
}

// validateSignerUnique checks if the signer is unique (not duplicated).
func (mv *messageValidator) validateSignerUnique(signer, prevSigner spectypes.OperatorID) error {
	if signer == prevSigner {
		return ErrDuplicatedSigner
	}
	return nil
}

// validateSignerIsKnown checks if the signer is known.
func (mv *messageValidator) validateSignerIsKnown(signer spectypes.OperatorID) error {
	exists, err := mv.operators.OperatorsExist(nil, []spectypes.OperatorID{signer})
	if err != nil {
		e := ErrOperatorValidation
		e.got = signer
		return e
	}

	if !exists {
		e := ErrUnknownOperator
		e.got = signer
		return e
	}

	return nil
}
