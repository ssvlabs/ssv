package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	SelectionProofPartialSig = spectypes.PartialSigMsgType(2) // Deprecated
	// ContributionProofs is the partial selection proofs for sync committee contributions (it's an array of sigs)
	ContributionProofs = spectypes.PartialSigMsgType(3) // Deprecated
	// ValidatorRegistrationPartialSig signs a validator registration, retired at Gloas. The Gloas-only
	// ssv-spec removed it but keeps its number reserved.
	ValidatorRegistrationPartialSig = spectypes.PartialSigMsgType(4)
)
