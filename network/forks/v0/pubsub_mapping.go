package v0

import (
	"encoding/hex"
)

// ValidatorTopicID - genesis version 0
func (v0 *ForkV0) ValidatorTopicID(pkByts []byte) []string {
	return []string{hex.EncodeToString(pkByts)}
}
