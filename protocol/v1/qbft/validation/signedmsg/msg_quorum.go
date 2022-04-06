package signedmsg

import (
	"errors"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
)

// ValidateQuorum is the pipeline to validate msg quorum requirement
func ValidateQuorum(threshold int) validation.SignedMessagePipeline {
	return validation.WrapFunc("quorum", func(signedMessage *message.SignedMessage) error {
		if len(signedMessage.GetSigners()) < threshold {
			return errors.New("quorum not achieved")
		}
		return nil
	})
}
