package auth

import (
	"bytes"

	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ValidateLambdas validates current and previous lambdas
func ValidateLambdas(lambda []byte) pipeline.Pipeline {
	return pipeline.WrapFunc("lambda", func(signedMessage *proto.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Lambda, lambda) {
			return errors.Errorf("message Lambda (%s) does not equal expected Lambda (%s)", string(signedMessage.Message.Lambda), string(lambda))
		}
		return nil
	})
}
