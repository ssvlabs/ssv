package runner

import (
	"encoding/hex"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func getPreConsensusSigners(state *State, root [32]byte) []spectypes.OperatorID {
	sigs := state.PreConsensusContainer.Signatures[state.StartingDuty.(*spectypes.BeaconDuty).ValidatorIndex][hex.EncodeToString(root[:])]
	var signers []spectypes.OperatorID
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}

func getPostConsensusSigners(state *State, root [32]byte) []spectypes.OperatorID {
	var valIdx phase0.ValidatorIndex
	switch state.StartingDuty.(type) {
	case *spectypes.BeaconDuty:
		valIdx = state.StartingDuty.(*spectypes.BeaconDuty).ValidatorIndex
	case *spectypes.CommitteeDuty:
		valIdx = state.StartingDuty.(*spectypes.CommitteeDuty).BeaconDuties[0].ValidatorIndex
	default:
		return nil
	}
	sigs := state.PostConsensusContainer.Signatures[valIdx][hex.EncodeToString(root[:])]
	var signers []spectypes.OperatorID
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}
