package v1

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	// UnknownSubnet is used when a validator public key is invalid
	UnknownSubnet = "unknown"
	decidedTopic  = "decided"

	topicPrefix = "ssv.v1"
)

// SubnetsCount returns the subnet count for v1
var SubnetsCount uint64 = 128

// GetTopicFullName returns the topic full name, including prefix
func (v1 *ForkV1) GetTopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
func (v1 *ForkV1) GetTopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}

// DecidedTopic returns decided topic name for v1
func (v1 *ForkV1) DecidedTopic() string {
	return decidedTopic
}

// ValidatorTopicID returns the topic to use for the given validator
func (v1 *ForkV1) ValidatorTopicID(pkByts []byte) []string {
	pkHex := hex.EncodeToString(pkByts)
	subnet := validatorSubnet(pkHex)
	return []string{pkHex, topicOf(subnet)}
}

// topicOf returns the topic for the given subnet
func topicOf(subnet int64) string {
	if subnet < 0 {
		return UnknownSubnet
	}
	return fmt.Sprintf("%d", subnet)
}

// validatorSubnet returns the subnet for the given validator
// TODO: allow reuse by other components
func validatorSubnet(validatorPKHex string) int64 {
	if len(validatorPKHex) < 10 {
		return -1
	}
	val := hexToUint64(validatorPKHex[:10])
	return int64(val % SubnetsCount)
}

func hexToUint64(hexStr string) uint64 {
	result, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		return uint64(0)
	}
	return result
}
