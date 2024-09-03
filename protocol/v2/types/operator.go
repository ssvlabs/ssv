package types

import spectypes "github.com/ssvlabs/ssv-spec/types"

func OperatorIDsFromOperators(operators []*spectypes.Operator) []spectypes.OperatorID {
	ids := make([]spectypes.OperatorID, len(operators))
	for i, op := range operators {
		ids[i] = op.OperatorID
	}
	return ids
}
