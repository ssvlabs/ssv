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

func getPostConsensusCommitteeSigners(state *State, root [32]byte) []spectypes.OperatorID {
	var signers []spectypes.OperatorID

	for _, bd := range state.StartingDuty.(*spectypes.CommitteeDuty).BeaconDuties {
		sigs := state.PostConsensusContainer.Signatures[bd.ValidatorIndex][hex.EncodeToString(root[:])]
		for op := range sigs {
			signers = append(signers, op)
		}
	}

	have := make(map[spectypes.OperatorID]struct{})
	var signersUnique []spectypes.OperatorID
	for _, opId := range signers {
		if _, ok := have[opId]; !ok {
			have[opId] = struct{}{}
			signersUnique = append(signersUnique, opId)
		}
	}

	return signersUnique
}

func getPostConsensusProposerSigners(state *State, root [32]byte) []spectypes.OperatorID {
	var signers []spectypes.OperatorID
	valIdx := state.StartingDuty.(*spectypes.BeaconDuty).ValidatorIndex
	sigs := state.PostConsensusContainer.Signatures[valIdx][hex.EncodeToString(root[:])]
	for op := range sigs {
		signers = append(signers, op)
	}

	return signers
}
