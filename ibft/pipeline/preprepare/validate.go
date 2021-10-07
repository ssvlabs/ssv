package preprepare

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// ValidatePrePrepareMsg validates pre-prepare message
func ValidatePrePrepareMsg(valueCheck valcheck.ValueCheck, expectedLeaderF func(round uint64) uint64) pipeline.Pipeline {
	return pipeline.WrapFunc("validate pre-prepare", func(signedMessage *proto.SignedMessage) error {
		if len(signedMessage.SignerIds) != 1 {
			return errors.New("invalid number of signers for pre-prepare message")
		}

		expectedLeader := expectedLeaderF(signedMessage.Message.Round)
		if signedMessage.SignerIds[0] != expectedLeader {
			return errors.New(fmt.Sprintf("pre-prepare message sender (id %d) is not the round's leader (expected %d)", signedMessage.SignerIds[0], expectedLeader))
		}

		if err := valueCheck.Check(signedMessage.Message.Value); err != nil {
			return errors.Wrap(err, "failed while validating pre-prepare")
		}

		return nil
	})
}
