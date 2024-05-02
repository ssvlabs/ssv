package runner

import (
	"encoding/hex"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

func getPreConsensusSigners(state *State, validatorIndex phase0.ValidatorIndex, root [32]byte) []spectypes.OperatorID {
	sigs := state.PreConsensusContainer.Signatures[validatorIndex][hex.EncodeToString(root[:])]
	var signers []spectypes.OperatorID
	for op := range sigs {
		signers = append(signers, op)
	}
	return signers
}
