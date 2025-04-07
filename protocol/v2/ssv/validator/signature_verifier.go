package validator

import (
	"fmt"

	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/utils/hashmap"
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

	encodedMsg, err := msg.SSVMessage.Encode()
	if err != nil {
		return err
	}

	verified := false
	// Find operator that matches ID with the signer and verify signature
	for i, signer := range msg.OperatorIDs {
		verified = true
		if err := s.VerifySignatureForSigner(encodedMsg, msg.Signatures[i], signer, operators); err != nil {
			return err
		}
	}

	if !verified {
		return fmt.Errorf("unknown signer")
	}
	return nil
}

func (s *SignatureVerifier) VerifySignatureForSigner(root []byte, signature []byte, signer spectypes.OperatorID, operators []*spectypes.Operator) error {

	for _, op := range operators {
		// Find signer
		if signer == op.OperatorID {
			operatorPubKey, ok := s.operatorIDToPubkeyCache.Get(signer)
			if !ok {
				var err error
				operatorPubKey, err = keys.PublicKeyFromString(string(op.SSVOperatorPubKey))
				if err != nil {
					return fmt.Errorf("could not parse signer public key: %w", err)
				}

				s.operatorIDToPubkeyCache.Set(op.OperatorID, operatorPubKey)
			}

			return operatorPubKey.Verify(root[:], signature)
		}
	}
	return errors.New("unknown signer")
}
