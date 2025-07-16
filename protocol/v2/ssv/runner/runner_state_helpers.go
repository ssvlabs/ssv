package runner

import (
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

func getPreConsensusSigners(state *State, root [32]byte) []spectypes.OperatorID {
	var signers []spectypes.OperatorID

	for op := range state.PreConsensusContainer.GetSignatures(state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex, root) {
		signers = append(signers, op)
	}

	return signers
}

func getPostConsensusCommitteeSigners(state *State, root [32]byte, logger *zap.Logger) []spectypes.OperatorID {
	var (
		have          = make(map[spectypes.OperatorID]struct{})
		signersUnique []spectypes.OperatorID
		iterations    uint64
		start         = time.Now()
	)

	for _, duty := range state.StartingDuty.(*spectypes.CommitteeDuty).ValidatorDuties {
		sigs := state.PostConsensusContainer.GetSignatures(duty.ValidatorIndex, root)
		for op := range sigs {
			iterations++
			if _, seen := have[op]; !seen {
				have[op] = struct{}{}
				signersUnique = append(signersUnique, op)
			}
		}
	}

	logger.Info("finished fetching unique signers", zap.Uint64("iterations", iterations), zap.Duration("elapsed", time.Since(start)))

	return signersUnique
}

func getPostConsensusProposerSigners(state *State, root [32]byte) []spectypes.OperatorID {
	var signers []spectypes.OperatorID

	for op := range state.PostConsensusContainer.GetSignatures(state.StartingDuty.(*spectypes.ValidatorDuty).ValidatorIndex, root) {
		signers = append(signers, op)
	}

	return signers
}
