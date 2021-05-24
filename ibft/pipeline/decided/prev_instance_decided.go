package decided

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
)

// PrevInstanceDecided verifies value is true
func PrevInstanceDecided(value bool) pipeline.Pipeline {
	return pipeline.WrapFunc("verify true", func(signedMessage *proto.SignedMessage) error {
		if !value {
			return errors.New("value is not true")
		}
		return nil
	})
}
