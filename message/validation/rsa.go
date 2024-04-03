package validation

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/operator/keys"
)

func (mv *messageValidator) verifySignature(msg *spectypes.SignedSSVMessage) error {
	operatorPubKey, ok := mv.operatorIDToPubkeyCache.Get(msg.OperatorID)
	if !ok {
		operator, found, err := mv.nodeStorage.GetOperatorData(nil, msg.OperatorID)
		if err != nil {
			e := ErrOperatorNotFound
			e.got = msg.OperatorID
			e.innerErr = err
			return e
		}
		if !found {
			e := ErrOperatorNotFound
			e.got = msg.OperatorID
			return e
		}

		operatorPubKey, err = keys.PublicKeyFromString(string(operator.PublicKey))
		if err != nil {
			e := ErrSignatureVerification
			e.innerErr = fmt.Errorf("decode public key: %w", err)
			return e
		}

		mv.operatorIDToPubkeyCache.Set(msg.OperatorID, operatorPubKey)
	}

	if err := operatorPubKey.Verify(msg.Data, msg.Signature); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", msg.OperatorID, err)
		return e
	}

	return nil
}
