package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	SelectionProofPartialSig = spectypes.PartialSigMsgType(2) // Deprecated
	// ContributionProofs is the partial selection proofs for sync committee contributions (it's an array of sigs)
	ContributionProofs = spectypes.PartialSigMsgType(3) // Deprecated
)
