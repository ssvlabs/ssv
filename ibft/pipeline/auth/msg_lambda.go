package auth

import (
	"bytes"
	"strings"

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

// ValidateLambdasRole validates msg role with ibft controller role
func ValidateLambdasRole(role string) pipeline.Pipeline {
	return pipeline.WrapFunc("lambda", func(signedMessage *proto.SignedMessage) error {
		msgRole := strings.Split(string(signedMessage.Message.Lambda), "_")
		if len(msgRole) < 2{
			return errors.Errorf("message Lambda Role not contain enough args")
		}
		if !strings.EqualFold(msgRole[1], role) {
			return errors.Errorf("message Lambda Role (%s) does not equal expected Role (%s)", msgRole, role)
		}
		return nil
	})
}
