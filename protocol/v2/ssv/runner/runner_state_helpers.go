package runner

import (
	"encoding/hex"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

func getPostConsensusSigners(state *State, root []byte) []spectypes.OperatorID {
	sigs := state.PostConsensusContainer.Signatures[hex.EncodeToString(root)]
	var signers []spectypes.OperatorID
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}
