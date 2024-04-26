package format

import (
	"encoding/hex"
	"errors"
	"strings"

	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

type DomainType spectypes.DomainType

func (d DomainType) String() string {
	return hex.EncodeToString(d[:])
}

func DomainTypeFromString(s string) (DomainType, error) {
	var d DomainType
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return d, err
	}
	if len(b) != len(d) {
		return d, errors.New("invalid domain type length")
	}
	copy(d[:], b)
	return d, nil
}
