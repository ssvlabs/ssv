package commons

import (
	"fmt"
	"strings"
)

const topicPrefix = "bloxstaking.ssv"

// TopicFullName returns the topic full name, including prefix
func TopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// TopicBaseName return the base topic name of the topic, w/o ssv prefix
func TopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}
