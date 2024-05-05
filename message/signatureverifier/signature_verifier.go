package signatureverifier

import (
	"fmt"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/operator/keys"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

//go:generate mockgen -package=signatureverifier -destination=./mock.go -source=./signature_verifier.go

type SignatureVerifier interface {
	VerifySignature(operatorID spectypes.OperatorID, message *spectypes.SSVMessage, signature []byte) error
}

type OperatorStore interface {
	GetOperatorData(r basedb.Reader, id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error)
}

type signatureVerifier struct {
	operatorIDToPubkeyCache   map[spectypes.OperatorID]keys.OperatorPublicKey
	operatorIDToPubkeyCacheMu sync.Mutex
	operatorStore             OperatorStore
}

func NewSignatureVerifier(operatorStore OperatorStore) SignatureVerifier {
	return &signatureVerifier{
		operatorIDToPubkeyCache: make(map[spectypes.OperatorID]keys.OperatorPublicKey),
		operatorStore:           operatorStore,
	}
}

func (sv *signatureVerifier) VerifySignature(operatorID spectypes.OperatorID, message *spectypes.SSVMessage, signature []byte) error {
	if len(signature) != 256 {
		return fmt.Errorf("invalid signature length")
	}

	sv.operatorIDToPubkeyCacheMu.Lock()
	operatorPubKey, ok := sv.operatorIDToPubkeyCache[operatorID]
	sv.operatorIDToPubkeyCacheMu.Unlock()
	if !ok {
		operator, found, err := sv.operatorStore.GetOperatorData(nil, operatorID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("operator not found")
		}

		operatorPubKey, err = keys.PublicKeyFromString(string(operator.PublicKey))
		if err != nil {
			return err
		}

		sv.operatorIDToPubkeyCacheMu.Lock()
		sv.operatorIDToPubkeyCache[operatorID] = operatorPubKey
		sv.operatorIDToPubkeyCacheMu.Unlock()
	}

	encodedMsg, err := message.Encode()
	if err != nil {
		return err
	}

	return operatorPubKey.Verify(encodedMsg, [256]byte(signature))
}
