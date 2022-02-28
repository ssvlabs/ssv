package v0

import (
	"encoding/hex"
)

// TODO: change to subnets

// ValidatorTopicID - genesis version 0
func (v1 *ForkV1) ValidatorTopicID(pkByts []byte) string {
	return hex.EncodeToString(pkByts)
}
