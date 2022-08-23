package changeround

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// uponFullQuorum implements pipeline.Pipeline interface
type uponFullQuorum struct {
	logger *zap.Logger
}

// UponFullQuorum is the constructor of uponFullQuorum
func UponFullQuorum(logger *zap.Logger) pipelines.SignedMessagePipeline {
	return &uponFullQuorum{
		logger: logger,
	}
}

// Run implements pipeline.Pipeline interface
//
// upon receiving a quorum Qrc of valid ⟨ROUND-CHANGE, λi, ri, −, −⟩ messages such that
//	leader(λi, ri) = pi ∧ JustifyRoundChange(Qrc) do
//		if HighestPrepared(Qrc) ̸= ⊥ then
//			let v such that (−, v) = HighestPrepared(Qrc))
//		else
//			let v such that v = inputValue i
//		broadcast ⟨PROPOSAL, λi, ri, v⟩
func (p *uponFullQuorum) Run(signedMessage *specqbft.SignedMessage) error {
	panic("not implemented yet")
}

// Name implements pipeline.Pipeline interface
func (p *uponFullQuorum) Name() string {
	return "upon full quorum"
}
