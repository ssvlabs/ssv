package v2

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

	topicPrefix = "ssv.v1.1"
)

// subnetsCount returns the subnet count for v2
var subnetsCount uint64 = 128

// ValidatorTopicID returns the topic to use for the given validator
func (v2 *ForkV2) ValidatorTopicID(pkByts []byte) []string {
	pkHex := hex.EncodeToString(pkByts)
	subnet := v2.ValidatorSubnet(pkHex)
	return []string{topicOf(subnet)}
}

// GetTopicFullName returns the topic full name, including prefix
func (v2 *ForkV2) GetTopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
func (v2 *ForkV2) GetTopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}

// DecidedTopic returns decided topic name for v1
func (v2 *ForkV2) DecidedTopic() string {
	return decidedTopic
}

// topicOf returns the topic for the given subnet
func topicOf(subnet int) string {
	if subnet < 0 {
		return UnknownSubnet
	}
	return fmt.Sprintf("%d", subnet)
}

// ValidatorSubnet returns the subnet for the given validator
func (v2 *ForkV2) ValidatorSubnet(validatorPKHex string) int {
	if len(validatorPKHex) < 10 {
		return -1
	}
	val := hexToUint64(validatorPKHex[:10])
	return int(val % subnetsCount)
}

func hexToUint64(hexStr string) uint64 {
	result, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		return uint64(0)
	}
	return result
}
