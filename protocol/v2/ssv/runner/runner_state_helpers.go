package runner

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func getPreConsensusSigners(state *State, root [32]byte) []spectypes.OperatorID {
	var signers []spectypes.OperatorID

	for op := range state.PreConsensusContainer.GetSignatures(state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex, root) {
		signers = append(signers, op)
	}

	return signers
}

func getPostConsensusCommitteeSigners(state *State, root [32]byte) []spectypes.OperatorID {
	var (
		have          = make(map[spectypes.OperatorID]struct{})
		signersUnique []spectypes.OperatorID
	)

	for _, duty := range state.StartingDuty.(*spectypes.CommitteeDuty).ValidatorDuties {
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
	var signers []spectypes.OperatorID

	for op := range state.PostConsensusContainer.GetSignatures(state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex, root) {
		signers = append(signers, op)
	}

	return signers
}
