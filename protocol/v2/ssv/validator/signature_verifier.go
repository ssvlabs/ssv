package validator

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/operator/keys"
)

type SignatureVerifier struct {
	OperatorPubKey keys.OperatorPublicKey
}

func (s *SignatureVerifier) Verify(msg *spectypes.SignedSSVMessage, operators []*spectypes.Operator) error {
	// Find operator that matches ID with the signer and verify signature
	for _, op := range operators {
		// Find operator
		if op.OperatorID == msg.GetOperatorID() {
			return s.OperatorPubKey.Verify(msg.Data, msg.Signature)
		}
	}

	return fmt.Errorf("unknown signer")
}
