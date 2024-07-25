package runner

import (
	"encoding/hex"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

func getPreConsensusSigners(state *State, root [32]byte) []genesisspectypes.OperatorID {
	sigs := state.PreConsensusContainer.Signatures[hex.EncodeToString(root[:])]
	var signers []genesisspectypes.OperatorID
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}

func getPostConsensusSigners(state *State, root [32]byte) []genesisspectypes.OperatorID {
	sigs := state.PostConsensusContainer.Signatures[hex.EncodeToString(root[:])]
	var signers []genesisspectypes.OperatorID
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}
