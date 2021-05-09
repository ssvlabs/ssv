package auth

import (
	"bytes"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
)

// ValidatePKs validates a msgs pk
func ValidatePKs(state *proto.State) pipeline.Pipeline {
	return pipeline.WrapFunc("validator PK", func(signedMessage *proto.SignedMessage) error {
		if len(signedMessage.Message.ValidatorPk) != 48 || !bytes.Equal(state.ValidatorPk, signedMessage.Message.ValidatorPk) {
			return errors.New("invalid message validator PK")
		}
		return nil
	})
}
