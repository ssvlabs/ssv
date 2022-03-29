package p2p

import (
	"fmt"
	"strings"
)

const (
	// SubnetsCount is the count of subnets in the network
	SubnetsCount = 128
	// subnetTopicPrefix is the prefix used for subnets
	subnetTopicPrefix = "ssv.subnet" // "bloxstaking.ssv"
)

// WrapTopicName returns the topic full name, including prefix
func WrapTopicName(baseName string) string {
	return fmt.Sprintf("%s.%s", subnetTopicPrefix, baseName)
}

// UnwrapTopicBaseName return the base topic name of the topic, w/o ssv prefix
func UnwrapTopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", subnetTopicPrefix), "", 1)
}
