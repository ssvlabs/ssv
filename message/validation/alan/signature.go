package validation

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/alan/types"

	"github.com/bloxapp/ssv/operator/keys"
)

func (mv *messageValidator) verifySignature(ssvMessage *spectypes.SSVMessage, operatorID spectypes.OperatorID, signature []byte) error {
	mv.metrics.MessageValidationRSAVerifications()

	operatorPubKey, ok := mv.operatorIDToPubkeyCache.Get(operatorID)
	if !ok {
		operator, found, err := mv.operatorStore.GetOperatorData(operatorID)
		if err != nil {
			e := ErrOperatorNotFound
			e.got = operatorID
			e.innerErr = err
			return e
		}
		if !found {
			e := ErrOperatorNotFound
			e.got = operatorID
			return e
		}

		operatorPubKey, err = keys.PublicKeyFromString(string(operator.PublicKey))
		if err != nil {
			e := ErrSignatureVerification
			e.innerErr = fmt.Errorf("decode public key: %w", err)
			return e
		}

		mv.operatorIDToPubkeyCache.Set(operatorID, operatorPubKey)
	}

	encodedMsg, err := ssvMessage.Encode() // TODO: should we abstract this?
	if err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("encode ssv message: %w", err)
		return e
	}

	if err := operatorPubKey.Verify(encodedMsg, signature); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", operatorID, err)
		return e
	}

	return nil
}
