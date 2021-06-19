package auth

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
)

// ValidateSequenceNumber validates msg seq number
func ValidateSequenceNumber(seq uint64) pipeline.Pipeline {
	return pipeline.WrapFunc("sequence", func(signedMessage *proto.SignedMessage) error {
		if signedMessage.Message.SeqNumber != seq {
			err := errors.Errorf("expected: %d, actual: %d",
				seq, signedMessage.Message.SeqNumber)
			return errors.Wrap(err, "invalid message sequence number")
		}
		return nil
	})
}
