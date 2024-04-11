package validator

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

type SignatureVerifier struct {
}

func (s *SignatureVerifier) Verify(msg *spectypes.SignedSSVMessage, operators []*spectypes.Operator) error {
	// Find operator that matches ID with the signer and verify signature
	for _, op := range operators {
		// Find operator
		if op.OperatorID == msg.GetOperatorID() {

			parsedPk, err := x509.ParsePKIXPublicKey(op.SSVOperatorPubKey)
			if err != nil {
				return fmt.Errorf("could not parse signer public key: %w", err)
			}

			pk, ok := parsedPk.(*rsa.PublicKey)
			if !ok {
				return fmt.Errorf("could not parse signer public key")
			}

			hash := sha256.Sum256(msg.Data)

			// Verify
			if err := rsa.VerifyPKCS1v15(pk, crypto.SHA256, hash[:], msg.Signature[:]); err != nil {
				return fmt.Errorf("could not verify signature: %w", err)
			}
		}
	}

	return nil
}
