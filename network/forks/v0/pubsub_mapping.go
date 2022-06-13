package v0

import (
	"encoding/hex"
)

// DecidedTopic implements forks.Fork, for v0 there is no decided topic
func (v0 *ForkV0) DecidedTopic() string {
	return ""
}

// ValidatorTopicID - genesis version 0
func (v0 *ForkV0) ValidatorTopicID(pkByts []byte) []string {
	return []string{hex.EncodeToString(pkByts)}
}
