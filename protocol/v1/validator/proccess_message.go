package validator

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/pkg/errors"
)

func (v *Validator) processConsensusMsg(controller controller.IController, signedMessage *message.SignedMessage) error {
	decided, decidedValueBytes, err := controller.ProcessMsg(signedMessage)
	if err != nil {
		return err
	}
	if decided {

	}
	//	 TODO handle decided and decidedValueBytes
	return nil
}

func (v *Validator) processPostConsensusSig(ibftController controller.IController, signedPostConsensusMessage *message.SignedPostConsensusMessage) error {
	return v.processSignatureMessage(signedPostConsensusMessage)
}

func (v *Validator) validateMessage(msg *message.SSVMessage) error {
	if !v.share.PublicKey.MessageIDBelongs(msg.GetIdentifier()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if v.DutyRunners.DutyRunnerForMsgID(msg.GetIdentifier()) == nil {
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
