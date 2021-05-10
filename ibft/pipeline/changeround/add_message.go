package changeround

import (
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/msgcont"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// addChangeRoundMessage implements pipeline.Pipeline interface
type addChangeRoundMessage struct {
	logger              *zap.Logger
	changeRoundMessages msgcont.MessageContainer
	state               *proto.State
}

// AddChangeRoundMessage is the constructor of addChangeRoundMessage
func AddChangeRoundMessage(logger *zap.Logger, changeRoundMessages msgcont.MessageContainer, state *proto.State) pipeline.Pipeline {
	return &addChangeRoundMessage{
		logger:              logger,
		changeRoundMessages: changeRoundMessages,
		state:               state,
	}
}

// Run implements pipeline.Pipeline interface
func (p *addChangeRoundMessage) Run(signedMessage *proto.SignedMessage) error {
	// TODO - if instance decidedChan should we process round change?
	if p.state.Stage == proto.RoundState_Decided {
		// TODO - can't get here, fails on round verification in pipeline
		p.logger.Info("received change round after decision, sending decidedChan message")
		return nil
	}

	// add to prepare messages
	p.changeRoundMessages.AddMessage(signedMessage)
	p.logger.Info("received valid change round message for round",
		zap.String("ibft_id", signedMessage.SignersIDString()),
		zap.Uint64("round", signedMessage.Message.Round))

	return nil
}

// Name implements pipeline.Pipeline interface
func (p *addChangeRoundMessage) Name() string {
	return "add change round msg"
}
