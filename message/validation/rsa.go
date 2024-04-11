package validation

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/operator/keys"
)

func (mv *messageValidator) verifySignature(messageData []byte, operatorID spectypes.OperatorID, signature [256]byte) error {
	operatorPubKey, ok := mv.operatorIDToPubkeyCache.Get(operatorID)
	if !ok {
		operator, found, err := mv.nodeStorage.GetOperatorData(nil, operatorID)
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

	if err := operatorPubKey.Verify(messageData, signature); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", operatorID, err)
		return e
	}

	return nil
}
