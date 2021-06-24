package ibft

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
)

// IdentifierFormat return base format for lambda
func IdentifierFormat(pubKey []byte, role beacon.Role) string {
	return fmt.Sprintf("%s_%s", hex.EncodeToString(pubKey), role.String())
}