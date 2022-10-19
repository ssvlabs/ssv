package instance

import (
	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
)

//TODO-spec copy-paste
func proposer(state *qbftspec.State, config qbftspec.IConfig, round qbftspec.Round) types.OperatorID {
	// TODO - https://github.com/ConsenSys/qbft-formal-spec-and-verification/blob/29ae5a44551466453a84d4d17b9e083ecf189d97/dafny/spec/L1/node_auxiliary_functions.dfy#L304-L323
	return config.GetProposerF()(state, round)
}