package validation

import (
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/keys"
)

func (mv *messageValidator) verifySignature(msg *spectypes.SignedSSVMessage) error {
	operatorID := msg.GetOperatorID()
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

	if err := operatorPubKey.Verify(msg.GetData(), msg.GetSignature()); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", operatorID, err)
		return e
	}

	return nil
}
