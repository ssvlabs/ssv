package message

import (
	"sort"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// Aggregate is a utility that helps to ensure sorted signers
func Aggregate(signedMsg *specqbft.SignedMessage, s spectypes.MessageSignature) error {
	if err := signedMsg.Aggregate(s); err != nil {
		return err
	}
	sort.Slice(signedMsg.Signers, func(i, j int) bool {
		return signedMsg.Signers[i] < signedMsg.Signers[j]
	})
	return nil
}
