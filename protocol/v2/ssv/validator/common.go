package validator

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
)

func ValidateMessage(share types.Share, msg *types.SSVMessage) error {
	if !share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
