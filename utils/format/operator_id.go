package format

import (
	"crypto/sha256"
	"fmt"
)

// OperatorID returns sha256 of the given operator public key
func OperatorID(operatorPubKey []byte) string {
	if operatorPubKey == nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(operatorPubKey))
}
