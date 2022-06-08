package v0

import (
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/utils/commons"
	scrypto "github.com/bloxapp/ssv/utils/crypto"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// GenUserAgent creates user agent string (<app-name>:<version>:<type>:<operator-id>)
func GenUserAgent(sk *rsa.PrivateKey) string {
	if sk != nil {
		operatorPubKey, err := rsaencryption.ExtractPublicKey(sk)
		if err != nil || len(operatorPubKey) == 0 {
			return GenUserAgentWithOperatorID("")
		}
		return GenUserAgentWithOperatorID(operatorID(hex.EncodeToString([]byte(operatorPubKey))))
	}
	return GenUserAgentWithOperatorID("")
}

// GenUserAgentWithOperatorID returns user agent string (<app-name>:<version>:<type>:<operator-id>) based on the given operator id
func GenUserAgentWithOperatorID(oid string) string {
	ua := commons.GetBuildData()
	if len(oid) > 0 {
		ua = fmt.Sprintf("%s:%s", ua, "operator")
		ua = fmt.Sprintf("%s:%s", ua, oid)
		return ua
	}
	return fmt.Sprintf("%s:%s", ua, "exporter")
}

func operatorID(pkHex string) string {
	if len(pkHex) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", scrypto.Sha256Hash([]byte(pkHex)))
}
