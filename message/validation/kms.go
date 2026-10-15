package validation

// kms.go validates friends-mode dKMS messages (role RoleKMS). They are not tied to validators or
// slots: a message belongs to a dKMS key, whose committee comes from the KMS key registry.

import (
	"bytes"
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/kms/kmstypes"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// KMSCommittees resolves the operator committee of a dKMS key.
type KMSCommittees interface {
	KMSCommittee(keyID [32]byte) ([]spectypes.OperatorID, bool)
}

// WithKMS enables validation of dKMS messages against the given key registry. Without it, dKMS
// messages are rejected like any unknown role.
func WithKMS(k KMSCommittees) Option {
	return func(mv *messageValidator) {
		mv.kms = k
	}
}

var (
	ErrUnknownKMSKey = Error{text: "unknown dKMS key"}
	ErrKMSMessage    = Error{text: "invalid dKMS message", reject: true}
)

func (mv *messageValidator) validateKMSMessage(signed *spectypes.SignedSSVMessage, decoded *queue.SSVMessage) (*queue.SSVMessage, error) {
	if mv.kms == nil {
		e := ErrInvalidRole
		e.got = "KMS (dKMS not enabled)"
		return decoded, e
	}
	msg := signed.SSVMessage
	if len(msg.Data) == 0 {
		return decoded, ErrEmptyData
	}
	if len(msg.Data) > kmstypes.MaxMessageSize {
		e := ErrSSVDataTooBig
		e.got = len(msg.Data)
		e.want = kmstypes.MaxMessageSize
		return decoded, e
	}
	if err := mv.validateDomainAllowlist(msg.GetID()); err != nil {
		return decoded, err
	}
	if msg.MsgType != spectypes.SSVConsensusMsgType && msg.MsgType != ssvmessage.SSVKMSMsgType {
		e := ErrUnknownSSVMessageType
		e.got = msg.MsgType
		return decoded, e
	}

	committee, ok := mv.kms.KMSCommittee(kmstypes.KeyIDFromMessageID(msg.GetID()))
	if !ok {
		return decoded, ErrUnknownKMSKey
	}
	if err := mv.belongsToCommittee(signed.OperatorIDs, committee); err != nil {
		return decoded, err
	}
	for i, signer := range signed.OperatorIDs {
		if err := mv.signatureVerifier.VerifySignature(signer, msg, signed.Signatures[i]); err != nil {
			e := ErrSignatureVerification
			e.innerErr = fmt.Errorf("verify operator %d: %w", signer, err)
			return decoded, e
		}
	}

	body, err := queue.ExtractMsgBody(msg)
	if err != nil {
		e := ErrKMSMessage
		e.innerErr = err
		return decoded, e
	}
	decoded.Body = body

	switch b := body.(type) {
	case *specqbft.Message:
		// Rule: the QBFT identifier must be the message ID it travels under.
		if !bytes.Equal(b.Identifier, msg.MsgID[:]) {
			e := ErrKMSMessage
			e.innerErr = fmt.Errorf("qbft identifier does not match message id")
			return decoded, e
		}
		// Only decided (aggregated commit) messages carry more than one signer.
		if len(signed.OperatorIDs) > 1 && b.MsgType != specqbft.CommitMsgType {
			return decoded, ErrNonDecidedWithMultipleSigners
		}
	case *kmstypes.Message:
		if len(signed.OperatorIDs) != 1 {
			e := ErrKMSMessage
			e.innerErr = fmt.Errorf("dKMS message must have one signer")
			return decoded, e
		}
	}
	return decoded, nil
}
