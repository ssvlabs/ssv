package v0

import (
	"crypto/rsa"
	"fmt"
	"github.com/bloxapp/ssv/utils/commons"
	scrypto "github.com/bloxapp/ssv/utils/crypto"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// GenerateUserAgent creates user agent string (<app-name>:<version>:<type>:<operator-id>)
func GenerateUserAgent(sk *rsa.PrivateKey) string {
	ua := commons.GetBuildData()
	if sk != nil {
		operatorPubKey, err := rsaencryption.ExtractPublicKey(sk)
		if err != nil || len(operatorPubKey) == 0 {
			return ua
		}
		ua = fmt.Sprintf("%s:%s", ua, "operator")
		ua = fmt.Sprintf("%s:%s", ua, operatorID(operatorPubKey))
		return ua
	}
	return fmt.Sprintf("%s:%s", ua, "exporter")
}

func operatorID(pkHex string) string {
	return fmt.Sprintf("%x", scrypto.Sha256Hash([]byte(pkHex)))
}
