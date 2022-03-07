package v0

import (
	"encoding/hex"
	"fmt"
	"strconv"
)

const (
	// UnknownSubnet is used when a validator public key is invalid
	UnknownSubnet = "unknown"
)

// SubnetCount returns the subnet count for v1
// TODO: change
var SubnetCount uint64 = 128

// ValidatorTopicID returns the topic to use for the given validator
func (v1 *ForkV1) ValidatorTopicID(pkByts []byte) string {
	subnet := validatorSubnet(hex.EncodeToString(pkByts), SubnetCount)
	return topicOf(subnet)
}

// topicOf returns the topic for the given subnet
// TODO: allow reuse by other components
func topicOf(subnet int64) string {
	if subnet < 0 {
		return UnknownSubnet
	}
	return fmt.Sprintf("ssv.subnet.%d", subnet)
}

// validatorSubnet returns the subnet for the given validator
// TODO: allow reuse by other components
func validatorSubnet(validatorPKHex string, n uint64) int64 {
	if len(validatorPKHex) < 10 {
		return -1
	}
	val := hexToUint64(validatorPKHex[:10])
	return int64(val % n)
}

func hexToUint64(hexStr string) uint64 {
	result, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		return uint64(0)
	}
	return result
}
