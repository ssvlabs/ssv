package format

import (
	"crypto/sha256"
	"fmt"
)

// OperatorPubKeyHash returns sha256 of the given operator public key
func OperatorPubKeyHash(operatorPubKey string) string {
	if operatorPubKey == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(operatorPubKey)))
}
