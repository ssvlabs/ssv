package validator

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/operator/keys"
)

type SignatureVerifier struct {
}

func (s *SignatureVerifier) Verify(msg *spectypes.SignedSSVMessage, operators []*spectypes.Operator) error {
	// Find operator that matches ID with the signer and verify signature
	for _, op := range operators {
		// Find operator
		if op.OperatorID == msg.GetOperatorID() {
			parsedPK, err := keys.PublicKeyFromBytes(op.SSVOperatorPubKey)
			if err != nil {
				return fmt.Errorf("could not parse signer public key: %w", err)
			}

			if err := parsedPK.Verify(msg.Data, msg.Signature); err != nil {
				return fmt.Errorf("could not verify signature: %w", err)
			}
		}
	}

	return nil
}
