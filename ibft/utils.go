package ibft

import (
	"fmt"
	"github.com/bloxapp/ssv/beacon"
)

// IdentifierFormat return base format for lambda
func IdentifierFormat(slot uint64, role beacon.Role) string {
	return fmt.Sprintf("%d_%s", slot, role.String())
}

