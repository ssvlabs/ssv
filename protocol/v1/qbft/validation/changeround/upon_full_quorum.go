package changeround

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"

	"go.uber.org/zap"
)

// uponFullQuorum implements pipeline.Pipeline interface
type uponFullQuorum struct {
	logger *zap.Logger
}

// UponFullQuorum is the constructor of uponFullQuorum
func UponFullQuorum(logger *zap.Logger) validation.SignedMessagePipeline {
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
//		broadcast ⟨PRE-PREPARE, λi, ri, v⟩
func (p *uponFullQuorum) Run(signedMessage *message.SignedMessage) error {
	panic("not implemented yet")
}

// Name implements pipeline.Pipeline interface
func (p *uponFullQuorum) Name() string {
	return "upon full quorum"
}
