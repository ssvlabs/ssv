package validator

import (
	"crypto/sha256"
	"fmt"
	"github.com/pkg/errors"

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

func (s *SignatureVerifier) Verify(msg *spectypes.SignedSSVMessage, operators []*spectypes.CommitteeMember) error {
	// Find operator that matches ID with the signer and verify signature

	encodedMsg, err := msg.SSVMessage.Encode()
	if err != nil {
		return err
	}

	// Get message hash
	hash := sha256.Sum256(encodedMsg)

	// Find operator that matches ID with the signer and verify signature
	for i, signer := range msg.OperatorIDs {
		if err := s.VerifySignatureForSigner(hash, msg.Signatures[i], signer, operators); err != nil {
			return err
		}
	}

	return fmt.Errorf("unknown signer")
}

func (s *SignatureVerifier) VerifySignatureForSigner(root [32]byte, signature []byte, signer spectypes.OperatorID, operators []*spectypes.CommitteeMember) error {

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
