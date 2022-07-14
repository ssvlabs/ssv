package genesis

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

	topicPrefix = "ssv.v2"
)

// subnetsCount returns the subnet count for genesis
var subnetsCount uint64 = 128

// ValidatorTopicID returns the topic to use for the given validator
func (genesis *ForkGenesis) ValidatorTopicID(pkByts []byte) []string {
	pkHex := hex.EncodeToString(pkByts)
	subnet := genesis.ValidatorSubnet(pkHex)
	return []string{topicOf(subnet)}
}

// GetTopicFullName returns the topic full name, including prefix
func (genesis *ForkGenesis) GetTopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
func (genesis *ForkGenesis) GetTopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}

// DecidedTopic returns decided topic name for v1
func (genesis *ForkGenesis) DecidedTopic() string {
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
func (genesis *ForkGenesis) ValidatorSubnet(validatorPKHex string) int {
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
