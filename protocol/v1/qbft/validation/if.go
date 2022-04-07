package validation

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// IfFirstTrueContinueToSecond runs pipeline a, if returns no error continues to pipeline b. otherwise returns without an error
func IfFirstTrueContinueToSecond(a, b SignedMessagePipeline) SignedMessagePipeline {
	return WrapFunc("if first pipeline non error, continue to second", func(signedMessage *message.SignedMessage) error {
		if a.Run(signedMessage) == nil {
			return b.Run(signedMessage)
		}
		return nil
	})
}
