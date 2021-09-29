package format

import (
	"encoding/hex"
	"fmt"
	"regexp"
)

// IdentifierFormat return base format for lambda
func IdentifierFormat(pubKey []byte, roleType string) string {
	return fmt.Sprintf("%s_%s", hex.EncodeToString(pubKey), roleType)
}

var (
	identifierRegexp = regexp.MustCompile("(.+)_(ATTESTER)|(PROPOSER)|(AGGREGATOR)")
)

// IdentifierUnformat return parts of the given lambda
func IdentifierUnformat(identifier string) (string, string) {
	parts := identifierRegexp.FindStringSubmatch(identifier)
	if len(parts) < 2 {
		return "", ""
	}
	return parts[1], parts[2]
}
