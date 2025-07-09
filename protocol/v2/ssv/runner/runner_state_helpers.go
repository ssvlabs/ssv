package runner

import (
	"encoding/hex"

	"github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func getPreConsensusSigners(state *State, root [32]byte) []spectypes.OperatorID {
	sigs := state.PreConsensusContainer.Signatures[state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex][ssv.SigningRoot(hex.EncodeToString(root[:]))]
	signers := make([]spectypes.OperatorID, 0, len(sigs))
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}

func getPostConsensusCommitteeSigners(state *State, root [32]byte) []spectypes.OperatorID {
	duties := state.StartingDuty.(*spectypes.CommitteeDuty).ValidatorDuties

	signers := make([]spectypes.OperatorID, 0, len(duties))

	for _, bd := range duties {
		sigs := state.PostConsensusContainer.Signatures[bd.ValidatorIndex][ssv.SigningRoot(hex.EncodeToString(root[:]))]
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
	valIdx := state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex
	sigs := state.PostConsensusContainer.Signatures[valIdx][ssv.SigningRoot(hex.EncodeToString(root[:]))]

	signers := make([]spectypes.OperatorID, 0, len(sigs))
	for op := range sigs {
		signers = append(signers, op)
	}

	return signers
}
