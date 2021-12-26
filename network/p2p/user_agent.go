package p2p

import (
	"crypto/rsa"
	"fmt"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"strings"
)

// UserAgent wraps a string with ua capabilities
type UserAgent string

// NewUserAgent wraps the given string as a UserAgent
func NewUserAgent(raw string) UserAgent {
	return UserAgent(raw)
}

// GenerateUserAgent creates user agent string (<app-name>:<version>:<type>:<public-key-hash>)
func GenerateUserAgent(sk *rsa.PrivateKey, ntype NodeType) (UserAgent, error) {
	ua := commons.GetBuildData()
	ua = fmt.Sprintf("%s:%s", ua, ntype.String())
	if sk != nil {
		operatorPubKey, err := rsaencryption.ExtractPublicKey(sk)
		if err != nil || len(operatorPubKey) == 0 {
			return NewUserAgent(ua), err
		}
		ua = fmt.Sprintf("%s:%s", ua, pubKeyHash(operatorPubKey))
	}
	return NewUserAgent(ua), nil
}

// NodeVersion returns the node version (e.g. v0.1.7)
func (ua UserAgent) NodeVersion() string {
	uaParts := strings.Split(string(ua), ":")
	if len(uaParts) > 1 {
		return uaParts[1]
	}
	return ""
}

// NodeType returns the node type ('operator' | 'exporter')
func (ua UserAgent) NodeType() string {
	uaParts := strings.Split(string(ua), ":")
	if len(uaParts) > 2 {
		return Unknown.FromString(uaParts[2]).String()
	}
	return Unknown.String()
}

// NodePubKeyHash returns public key hash or empty string if not available
func (ua UserAgent) NodePubKeyHash() string {
	uaParts := strings.Split(string(ua), ":")
	n := len(uaParts)
	if n > 2 {
		lastPart := uaParts[n-1]
		if lastPart == Operator.String() || lastPart == Exporter.String() || lastPart == Unknown.String() {
			// public key hash does not exist (probably due to older version), node type is the last entry
			return ""
		}
		return uaParts[n-1]
	}
	return ""
}
