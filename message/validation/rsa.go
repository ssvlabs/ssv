package validation

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/utils/rsaencryption"
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

		operatorPubKey, err := base64.StdEncoding.DecodeString(string(operator.PublicKey))
		if err != nil {
			e := ErrRSADecryption
			e.innerErr = fmt.Errorf("decode public key: %w", err)
			return e
		}

		rsaPubKey, err = rsaencryption.ConvertPemToPublicKey(operatorPubKey)
		if err != nil {
			e := ErrRSADecryption
			e.innerErr = fmt.Errorf("convert PEM: %w", err)
			return e
		}

		mv.operatorIDToPubkeyCache.Set(operatorID, rsaPubKey)
	}

	messageHash := sha256.Sum256(messageData)

	if err := rsa.VerifyPKCS1v15(rsaPubKey, crypto.SHA256, messageHash[:], signature); err != nil {
		e := ErrRSADecryption
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", operatorID, err)
		return e
	}

	return nil
}
