package auth

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
)

// ValidatePKs validates a msgs pk
func ValidatePKs(state *proto.State) pipeline.Pipeline {
	return pipeline.WrapFunc(func(signedMessage *proto.SignedMessage) error {
		if len(signedMessage.Message.ValidatorPk) != 48 {
			return errors.New("invalid message validator PK")
		}
		return nil
	})
}
