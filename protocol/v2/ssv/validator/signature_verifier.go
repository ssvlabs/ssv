package validator

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"

	"github.com/bloxapp/ssv/operator/keys"
)

type SignatureVerifier struct {
	operatorIDToPubkeyCache *hashmap.Map[spectypes.OperatorID, keys.OperatorPublicKey]
}

func NewSignatureVerifier() *SignatureVerifier {
	return &SignatureVerifier{
		operatorIDToPubkeyCache: hashmap.New[spectypes.OperatorID, keys.OperatorPublicKey](),
	}
}

func (s *SignatureVerifier) Verify(msg *spectypes.SignedSSVMessage, operators []*spectypes.Operator) error {
	// Find operator that matches ID with the signer and verify signature
	for _, op := range operators {
		if op.OperatorID != msg.GetOperatorID() {
			continue
		}
			
		operatorPubKey, ok := s.operatorIDToPubkeyCache.Get(op.OperatorID)
		if !ok {
			var err error
			operatorPubKey, err = keys.PublicKeyFromString(string(op.SSVOperatorPubKey))
			if err != nil {
				return fmt.Errorf("could not parse signer public key: %w", err)
			}
		
			s.operatorIDToPubkeyCache.Set(op.OperatorID, operatorPubKey)
		}

		return operatorPubKey.Verify(msg.Data, msg.Signature)
	}
	}

	return fmt.Errorf("unknown signer")
}
