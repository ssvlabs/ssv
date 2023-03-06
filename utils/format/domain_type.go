package format

import (
	"encoding/hex"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

type DomainType spectypes.DomainType

func (d DomainType) String() string {
	return hex.EncodeToString(d[:])
}

func DomainTypeFromString(s string) (DomainType, error) {
	var d DomainType
	b, err := hex.DecodeString(s)
	if err != nil {
		return d, err
	}
	copy(d[:], b)
	return d, nil
}
