package message

import (
	"sort"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Aggregate is a utility that helps to ensure sorted signers
func Aggregate(signedMsg *spectypes.SignedSSVMessage, msgToAggregate *spectypes.SignedSSVMessage) error {
	if err := signedMsg.Aggregate(msgToAggregate); err != nil {
		return err
	}
	sort.Slice(signedMsg.OperatorIDs, func(i, j int) bool {
		return signedMsg.OperatorIDs[i] < signedMsg.OperatorIDs[j]
	})
	return nil
}
