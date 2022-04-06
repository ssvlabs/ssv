package decided

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// ValidateDecidedMsg validates the given decided message with the corresponding share
func ValidateDecidedMsg(msg *message.SignedMessage, share *message.Share) error {
	p := validation.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.CommitMsgType), // TODO: decided type?
		//signed_msg.AuthorizeMsg(share), // TODO: enable after interface is fixed
		signedmsg.ValidateQuorum(share.ThresholdSize()),
	)
	return p.Run(msg)
}
