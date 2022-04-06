package validator

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ProcessMsg processes a new msg, returns true if Decided, non nil byte slice if Decided (Decided value) and error
// Decided returns just once per instance as true, following messages (for example additional commit msgs) will not return Decided true
func (v *Validator) ProcessMsg(msg *message.SSVMessage) /*(bool, []byte, error)*/ {
	// check duty type and handle accordingly
	if v.readMode {
		// synchronize process
		v.messageHandler(msg)
		return
	}
	// put msg to queue in order to preform async process and prevent blocking validatorController
	v.worker.TryEnqueue(msg)
}

// messageHandler process message from queue,
func (v *Validator) messageHandler(msg *message.SSVMessage) {
	// validation
	if err := v.validateMessage(msg); err != nil {
		// TODO need to return error?
		v.logger.Error("message validation failed", zap.Error(err))
		return
	}

	switch msg.GetType() {
	case message.SSVPostConsensusMsgType:
		// use DutyExecution func's to process msg
		break
	case message.SSVConsensusMsgType:
		// pass to ibft controller ProcessMessage()

		break
	case message.SSVSyncMsgType:
		// pass to ibft controller ProcessMessage() TODO ?
		break
	}
}

func (v *Validator) validateMessage(msg *message.SSVMessage) error {
	if !v.share.PublicKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if v.DutyRunners.DutyRunnerForMsgID(msg.GetID()) == nil {
		return errors.New("could not find duty runner for msg ID")
	}

	if msg.GetType() > 2 {
		return errors.New("msg type not supported")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
