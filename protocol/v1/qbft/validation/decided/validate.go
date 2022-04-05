package decided

import (
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signed_msg"
)

// ValidateDecidedMsg validates the given decided message with the corresponding share
func ValidateDecidedMsg(msg *message.SignedMessage, share *keymanager.Share) error {
	p := validation.Combine(
		signed_msg.BasicMsgValidation(),
		signed_msg.MsgTypeCheck(message.CommitMsgType), // TODO: decided type?
		//signed_msg.AuthorizeMsg(share), // TODO: enable after interface is fixed
		signed_msg.ValidateQuorum(share.ThresholdSize()),
	)
	return p.Run(msg)
}
