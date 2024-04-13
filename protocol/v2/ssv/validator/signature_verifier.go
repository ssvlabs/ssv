package validator

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"

	"github.com/bloxapp/ssv/operator/keys"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

type SignatureVerifier struct {
	nodeStorage             Operators
	operatorIDToPubkeyCache *hashmap.Map[spectypes.OperatorID, keys.OperatorPublicKey]
}

func NewSignatureVerifier(nodeStorage Operators) *SignatureVerifier {
	return &SignatureVerifier{
		nodeStorage:             nodeStorage,
		operatorIDToPubkeyCache: hashmap.New[spectypes.OperatorID, keys.OperatorPublicKey](),
	}
}

type Operators interface {
	GetOperatorData(r basedb.Reader, id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error)
}

func (s *SignatureVerifier) Verify(msg *spectypes.SignedSSVMessage, operators []*spectypes.Operator) error {
	// Find operator that matches ID with the signer and verify signature
	for _, op := range operators {
		if op.OperatorID == msg.GetOperatorID() {
			operatorPubKey, ok := s.operatorIDToPubkeyCache.Get(op.OperatorID)
			if !ok {
				operator, found, err := s.nodeStorage.GetOperatorData(nil, op.OperatorID)
				if err != nil {
					return fmt.Errorf("could not get operator data: %w", err)
				}
				if !found {
					return fmt.Errorf("operator not found: %v", op.OperatorID)
				}

				operatorPubKey, err = keys.PublicKeyFromString(string(operator.PublicKey))
				if err != nil {
					return fmt.Errorf("could not decode public key from string: %w", err)
				}

				s.operatorIDToPubkeyCache.Set(op.OperatorID, operatorPubKey)
			}

			return operatorPubKey.Verify(msg.Data, msg.Signature)
		}
	}

	return fmt.Errorf("unknown signer")
}
