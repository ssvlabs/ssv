package v0

import (
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	topicPrefix = "bloxstaking.ssv"
)

// GetTopicFullName returns the topic full name, including prefix
func (v0 *ForkV0) GetTopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
func (v0 *ForkV0) GetTopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}

// DecidedTopic implements forks.Fork, for v0 there is no decided topic
func (v0 *ForkV0) DecidedTopic() string {
	return ""
}

// ValidatorTopicID - genesis version 0
func (v0 *ForkV0) ValidatorTopicID(pkByts []byte) []string {
	return []string{hex.EncodeToString(pkByts)}
}
