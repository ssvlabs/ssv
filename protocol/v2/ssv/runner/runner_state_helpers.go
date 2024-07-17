package runner

import (
	"encoding/hex"

	"github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func getPreConsensusSigners(state *State, root [32]byte) []spectypes.OperatorID {
	sigs := state.PreConsensusContainer.Signatures[state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex][ssv.SigningRoot(hex.EncodeToString(root[:]))]
	var signers []spectypes.OperatorID
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}

func getPostConsensusSigners(state *State, root [32]byte) []spectypes.OperatorID {
	sigs := state.PostConsensusContainer.Signatures[state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex][ssv.SigningRoot(hex.EncodeToString(root[:]))]
	var signers []spectypes.OperatorID
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}
