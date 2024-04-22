package validation

import (
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
)

// TODO: rename
type Share interface {
	GetCommittee() []*spectypes.Operator
}
