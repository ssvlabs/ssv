package format

import (
	"crypto/sha256"
	"fmt"
)

// OperatorID returns sha256 of the given operator public key
func OperatorID(operatorPubkey string) string {
	if len(operatorPubkey) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(operatorPubkey)))
}
