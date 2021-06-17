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
			return errors.Errorf("invalid message sequence number; expected: %d, actual: %d", seq, signedMessage.Message.SeqNumber)
		}
		return nil
	})
}
