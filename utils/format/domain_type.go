package format

import (
	"encoding/hex"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type DomainType spectypes.DomainType

func (d DomainType) String() string {
	return hex.EncodeToString(d[:])
}
