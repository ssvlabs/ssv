package validation

import (
	"fmt"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/operator/keys"
)

type SignatureVerifier interface {
	VerifySignature(operatorID spectypes.OperatorID, message *spectypes.SSVMessage, signature []byte) error
}

// TODO: move signatureVerifier outside of message validation

type signatureVerifier struct {
	operatorIDToPubkeyCache   map[spectypes.OperatorID]keys.OperatorPublicKey
	operatorIDToPubkeyCacheMu sync.Mutex
	operatorStore             OperatorStore
}

func newSignatureVerifier(operatorStore OperatorStore) *signatureVerifier {
	return &signatureVerifier{
		operatorIDToPubkeyCache: make(map[spectypes.OperatorID]keys.OperatorPublicKey),
		operatorStore:           operatorStore,
	}
}

func (sv *signatureVerifier) VerifySignature(operatorID spectypes.OperatorID, message *spectypes.SSVMessage, signature []byte) error {
	sv.operatorIDToPubkeyCacheMu.Lock()
	operatorPubKey, ok := sv.operatorIDToPubkeyCache[operatorID]
	sv.operatorIDToPubkeyCacheMu.Unlock()
	if !ok {
		operator, found, err := sv.operatorStore.GetOperatorData(operatorID)
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

		sv.operatorIDToPubkeyCacheMu.Lock()
		sv.operatorIDToPubkeyCache[operatorID] = operatorPubKey
		sv.operatorIDToPubkeyCacheMu.Unlock()
	}

	encodedMsg, err := message.Encode()
	if err != nil {
		return err
	}

	return operatorPubKey.Verify(encodedMsg, signature)
}
