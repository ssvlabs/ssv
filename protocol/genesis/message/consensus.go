package message

import (
	"sort"

	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
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
