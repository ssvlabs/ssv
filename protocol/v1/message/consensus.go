package message

import (
	"sort"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

// AppendSigners is a utility that helps to ensure distinct values
// TODO: sorting?
func AppendSigners(signers []spectypes.OperatorID, appended ...spectypes.OperatorID) []spectypes.OperatorID {
	for _, signer := range appended {
		signers = appendSigner(signers, signer)
	}
	sort.Slice(signers, func(i, j int) bool {
		return signers[i] < signers[j]
	})
	return signers
}

func appendSigner(signers []spectypes.OperatorID, signer spectypes.OperatorID) []spectypes.OperatorID {
	for _, s := range signers {
		if s == signer { // known
			return signers
		}
	}
	return append(signers, signer)
}
