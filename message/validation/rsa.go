package validation

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/operator/keys"
)

func (mv *messageValidator) verifyRSASignature(messageData []byte, operatorID spectypes.OperatorID, signature []byte) error {
	rsaPubKey, ok := mv.operatorIDToPubkeyCache.Get(operatorID)
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

		rsaPubKey, err = keys.PublicKeyFromString(string(operator.PublicKey))
		if err != nil {
			e := ErrRSADecryption
			e.innerErr = fmt.Errorf("decode public key: %w", err)
			return e
		}

		mv.operatorIDToPubkeyCache.Set(operatorID, rsaPubKey)
	}

	if err := rsaPubKey.Verify(messageData, signature); err != nil {
		e := ErrRSADecryption
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", operatorID, err)
		return e
	}

	return nil
}
