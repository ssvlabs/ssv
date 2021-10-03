package pipeline

import (
	"github.com/bloxapp/ssv/ibft/proto"
)

// IfFirstTrueContinueToSecond runs pipeline a, if returns no error continues to pipeline b. otherwise returns without an error
func IfFirstTrueContinueToSecond(a, b Pipeline) Pipeline {
	return WrapFunc("if first pipeline non error, continue to second", func(signedMessage *proto.SignedMessage) error {
		if a.Run(signedMessage) == nil {
			return b.Run(signedMessage)
		}
		return nil
	})
}
