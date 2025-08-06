package runner

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func getPreConsensusSigners(state *State, root [32]byte) []spectypes.OperatorID {
	sigs := state.PreConsensusContainer.GetSignatures(state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex, root)
	signers := make([]spectypes.OperatorID, 0, len(sigs))
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}

func getPostConsensusCommitteeSigners(state *State, root [32]byte) []spectypes.OperatorID {
	duties := state.StartingDuty.(*spectypes.CommitteeDuty).ValidatorDuties

	have := make(map[spectypes.OperatorID]struct{}, len(duties))
	signersUnique := make([]spectypes.OperatorID, 0, len(duties))

	for _, duty := range duties {
		sigs := state.PostConsensusContainer.GetSignatures(duty.ValidatorIndex, root)
		for op := range sigs {
			if _, seen := have[op]; !seen {
				have[op] = struct{}{}
				signersUnique = append(signersUnique, op)
			}
		}
	}

	return signersUnique
}

func getPostConsensusProposerSigners(state *State, root [32]byte) []spectypes.OperatorID {
	sigs := state.PostConsensusContainer.GetSignatures(state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex, root)
	signers := make([]spectypes.OperatorID, 0, len(sigs))
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}
